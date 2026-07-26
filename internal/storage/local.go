package storage

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

type LocalStore struct {
	root string
}

func NewLocalStore(root string) (*LocalStore, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, errors.New("normalize local storage root failed")
	}
	return &LocalStore{root: filepath.Clean(absolute)}, nil
}

func (store *LocalStore) Put(ctx context.Context, request PutRequest) (object Object, returnErr error) {
	if err := validateRequest(request); err != nil {
		return Object{}, err
	}
	if err := os.MkdirAll(store.root, 0o750); err != nil {
		return Object{}, localOperationError("create root directory", err)
	}
	root, err := os.OpenRoot(store.root)
	if err != nil {
		return Object{}, localOperationError("open root directory", err)
	}
	defer root.Close()
	if existing, found, err := store.verifyExisting(ctx, root, request); err != nil || found {
		return existing, err
	}
	parent := path.Dir(request.Key)
	if err := root.MkdirAll(parent, 0o750); err != nil {
		return Object{}, localOperationError("create parent directory", err)
	}
	if err := syncLocalDirectoryChain(root, parent); err != nil {
		return Object{}, localOperationError("sync parent directories", err)
	}
	temporary, temporaryKey, err := createLocalTemp(root, parent)
	if err != nil {
		return Object{}, localOperationError("create temporary object", err)
	}
	closed := false
	defer func() {
		if !closed {
			returnErr = errors.Join(returnErr, localOperationError("close temporary object", temporary.Close()))
		}
		if err := root.Remove(temporaryKey); err != nil && !os.IsNotExist(err) {
			returnErr = errors.Join(returnErr, localOperationError("remove temporary object", err))
		}
	}()

	source, err := request.Source.Open()
	if err != nil {
		return Object{}, errors.New("open storage source failed")
	}
	defer source.Close()
	hash := sha256.New()
	written, err := io.Copy(io.MultiWriter(temporary, hash), contextReader{ctx: ctx, reader: source})
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return Object{}, ctxErr
		}
		return Object{}, errors.New("read storage source failed")
	}
	actualHash := hex.EncodeToString(hash.Sum(nil))
	if written != request.Size || actualHash != request.SHA256 {
		return Object{}, fmt.Errorf("%w: source size or SHA256 mismatch", ErrConflict)
	}
	if err := temporary.Sync(); err != nil {
		return Object{}, localOperationError("sync temporary object", err)
	}
	if err := temporary.Close(); err != nil {
		closed = true
		return Object{}, localOperationError("close temporary object", err)
	}
	closed = true
	if err := root.Link(temporaryKey, request.Key); err != nil {
		if existing, found, verifyErr := store.verifyExisting(ctx, root, request); verifyErr != nil || found {
			return existing, verifyErr
		}
		return Object{}, localOperationError("publish object", err)
	}
	if err := root.Remove(temporaryKey); err != nil {
		return Object{}, localOperationError("remove published temporary object", err)
	}
	if err := syncLocalDirectory(root, parent); err != nil {
		return Object{}, localOperationError("sync published object directory", err)
	}
	return newObject("local", request, time.Now().UTC(), "", "", Created(true)), nil
}

func (store *LocalStore) verifyExisting(ctx context.Context, root *os.Root, request PutRequest) (Object, bool, error) {
	file, err := root.Open(request.Key)
	if os.IsNotExist(err) {
		return Object{}, false, nil
	}
	if err != nil {
		return Object{}, false, localOperationError("open existing object", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return Object{}, false, localOperationError("inspect existing object", err)
	}
	if info.Size() != request.Size {
		return Object{}, true, fmt.Errorf("%w: existing object size differs", ErrConflict)
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, contextReader{ctx: ctx, reader: file}); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return Object{}, true, ctxErr
		}
		return Object{}, true, localOperationError("verify existing object", err)
	}
	if hex.EncodeToString(hash.Sum(nil)) != request.SHA256 {
		return Object{}, true, fmt.Errorf("%w: existing object SHA256 differs", ErrConflict)
	}
	return newObject("local", request, info.ModTime().UTC(), "", "", Created(false)), true, nil
}

func syncLocalDirectoryChain(root *os.Root, directory string) error {
	if err := syncLocalDirectory(root, "."); err != nil {
		return err
	}
	current := ""
	for _, segment := range strings.Split(directory, "/") {
		if segment == "." || segment == "" {
			continue
		}
		current = path.Join(current, segment)
		if err := syncLocalDirectory(root, current); err != nil {
			return err
		}
	}
	return nil
}

func syncLocalDirectory(root *os.Root, directory string) error {
	file, err := root.Open(directory)
	if err != nil {
		return err
	}
	defer file.Close()
	return file.Sync()
}

func createLocalTemp(root *os.Root, parent string) (*os.File, string, error) {
	for range 100 {
		var random [16]byte
		if _, err := rand.Read(random[:]); err != nil {
			return nil, "", err
		}
		name := path.Join(parent, ".raw-storage-"+hex.EncodeToString(random[:]))
		file, err := root.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			return file, name, nil
		}
		if !os.IsExist(err) {
			return nil, "", err
		}
	}
	return nil, "", errors.New("could not allocate a unique temporary object")
}

func localOperationError(operation string, err error) error {
	if err == nil {
		return nil
	}
	var pathError *os.PathError
	if errors.As(err, &pathError) {
		err = pathError.Err
	}
	return fmt.Errorf("local storage %s failed: %w", operation, err)
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (reader contextReader) Read(buffer []byte) (int, error) {
	if err := reader.ctx.Err(); err != nil {
		return 0, err
	}
	return reader.reader.Read(buffer)
}

func newObject(backend string, request PutRequest, storedAt time.Time, etag, versionID string, created *bool) Object {
	return Object{Backend: backend, Key: request.Key, Size: request.Size, SHA256: request.SHA256,
		StoredAt: storedAt, ETag: etag, VersionID: versionID, Created: created}
}
