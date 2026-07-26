package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/kevinle-00/transit-observatory/internal/storage"
)

var archiveHashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

const archiveCommitResolutionTimeout = 5 * time.Second

func (r *AlertRepository) RecordAlertArchive(ctx context.Context, runID int64, object storage.Object) error {
	return recordArchive(ctx, r.db, "ingestion_runs", runID, object)
}

func (r *GTFSRepository) RecordGTFSArchive(ctx context.Context, importID int64, object storage.Object) error {
	return recordArchive(ctx, r.db, "gtfs_imports", importID, object)
}

func recordArchive(ctx context.Context, db *sql.DB, attemptTable string, attemptID int64, object storage.Object) error {
	if object.Backend == "" || object.Key == "" || object.Size < 0 || object.StoredAt.IsZero() || !archiveHashPattern.MatchString(object.SHA256) {
		return errors.New("record raw archive: invalid storage object metadata")
	}
	if err := storage.ValidateKey(object.Key); err != nil {
		return fmt.Errorf("record raw archive: invalid object key: %w", err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin raw archive record: %w", err)
	}
	defer tx.Rollback()
	var archiveID int64
	var hash string
	var bytes int64
	var versionID sql.NullString
	err = tx.QueryRowContext(ctx, `
		INSERT INTO raw_archives (backend, object_key, content_sha256, bytes, stored_at, etag, version_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (backend, object_key) DO UPDATE SET object_key = EXCLUDED.object_key
		RETURNING id, content_sha256, bytes, version_id
	`, object.Backend, object.Key, object.SHA256, object.Size, object.StoredAt.UTC(),
		nullableString(object.ETag), nullableString(object.VersionID)).Scan(&archiveID, &hash, &bytes, &versionID)
	if err != nil {
		return fmt.Errorf("insert raw archive: %w", err)
	}
	if hash != object.SHA256 || bytes != object.Size || versionID.String != object.VersionID {
		return fmt.Errorf("record raw archive: %w", storage.ErrConflict)
	}
	query := fmt.Sprintf(`
		UPDATE %s SET archive_status = 'archived', raw_archive_id = $2,
			archive_created = $3, archive_error = NULL
		WHERE id = $1 AND status = 'running' AND archive_status = 'pending'
	`, attemptTable)
	result, err := tx.ExecContext(ctx, query, attemptID, archiveID, object.Created)
	if err != nil {
		return fmt.Errorf("link raw archive to attempt %d: %w", attemptID, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("inspect raw archive link: %w", err)
	}
	if affected == 0 {
		var existingID sql.NullInt64
		var status, archiveStatus string
		lookup := fmt.Sprintf(`SELECT status, archive_status, raw_archive_id FROM %s WHERE id = $1`, attemptTable)
		if err := tx.QueryRowContext(ctx, lookup, attemptID).Scan(&status, &archiveStatus, &existingID); err != nil {
			return fmt.Errorf("load archive attempt %d: %w", attemptID, err)
		}
		if archiveStatus != "archived" || !existingID.Valid || existingID.Int64 != archiveID {
			return fmt.Errorf("link raw archive to attempt %d: attempt is not matching running archive", attemptID)
		}
	}
	if err := tx.Commit(); err != nil {
		commitErr := fmt.Errorf("commit raw archive record: %w", err)
		resolutionContext, cancel := context.WithTimeout(context.WithoutCancel(ctx), archiveCommitResolutionTimeout)
		defer cancel()
		committed, resolutionErr := resolveArchiveCommit(resolutionContext, db, attemptTable, attemptID, object)
		if resolutionErr != nil {
			return archiveCommitOutcomeError{commitError: commitErr, resolutionError: resolutionErr}
		}
		if committed {
			return nil
		}
		return commitErr
	}
	return nil
}

func resolveArchiveCommit(ctx context.Context, db *sql.DB, attemptTable string, attemptID int64, object storage.Object) (bool, error) {
	query := fmt.Sprintf(`
		SELECT attempt.archive_status, archive.backend, archive.object_key,
			archive.content_sha256, archive.bytes, archive.version_id
		FROM %s attempt
		LEFT JOIN raw_archives archive ON archive.id = attempt.raw_archive_id
		WHERE attempt.id = $1
	`, attemptTable)
	var archiveStatus string
	var backend, key, hash, versionID sql.NullString
	var bytes sql.NullInt64
	if err := db.QueryRowContext(ctx, query, attemptID).Scan(&archiveStatus, &backend, &key, &hash, &bytes, &versionID); err != nil {
		return false, fmt.Errorf("resolve raw archive commit for attempt %d: %w", attemptID, err)
	}
	if archiveStatus != "archived" {
		return false, nil
	}
	return backend.Valid && backend.String == object.Backend && key.Valid && key.String == object.Key &&
		hash.Valid && hash.String == object.SHA256 && bytes.Valid && bytes.Int64 == object.Size &&
		versionID.String == object.VersionID, nil
}

type archiveCommitOutcomeError struct {
	commitError     error
	resolutionError error
}

func (e archiveCommitOutcomeError) Error() string {
	return fmt.Sprintf("raw archive commit outcome is unknown: commit: %v; status check: %v", e.commitError, e.resolutionError)
}

func (e archiveCommitOutcomeError) Unwrap() error { return e.commitError }

func (e archiveCommitOutcomeError) CommitOutcomeUnknown() bool { return true }
