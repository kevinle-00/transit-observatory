package database

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

var analyticsMetricLimitations = []string{
	"An alert is counted once each time it appears. If it disappears and later returns, it is counted again. Counts do not measure incidents or affected passengers.",
	"For alerts that ended, duration runs from the first to the last time the alert appeared. It does not include the time needed to confirm that the alert had ended.",
	"Older alerts use the current line list, which may differ from the lines in service when the alert appeared.",
}

func (r *ReadRepository) ListLineAnalytics(ctx context.Context, query AnalyticsQuery) ([]LineAnalytics, error) {
	tx, err := r.readTx(ctx, "line analytics list query")
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	lines, err := queryLines(ctx, tx, query.IncludeReplacementBus, query.Now.UTC(), "")
	if err != nil {
		return nil, err
	}
	results := make([]LineAnalytics, len(lines))
	indexes := make(map[string]int, len(lines))
	lineIDs := make([]string, len(lines))
	for index, line := range lines {
		lineIDs[index] = line.ID
		indexes[line.ID] = index
		results[index] = LineAnalytics{
			Line: line, Series: analyticsBuckets(query), Causes: []AnalyticsBreakdown{},
			Effects: []AnalyticsBreakdown{}, MetricLimitations: []string{},
		}
	}
	if len(lines) > 0 {
		if err := queryLineAnalyticsSeries(ctx, tx, query, lineIDs, indexes, results); err != nil {
			return nil, err
		}
		if err := queryLineAnalyticsBreakdown(ctx, tx, query, lineIDs, indexes, results, "cause"); err != nil {
			return nil, err
		}
		if err := queryLineAnalyticsBreakdown(ctx, tx, query, lineIDs, indexes, results, "effect"); err != nil {
			return nil, err
		}
	}
	if err := commitReadTx(tx, "line analytics list query"); err != nil {
		return nil, err
	}
	return results, nil
}

func analyticsBuckets(query AnalyticsQuery) []AnalyticsPoint {
	start := query.From.UTC()
	start = time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC)
	if query.Interval == "week" {
		start = start.AddDate(0, 0, -(int(start.Weekday())+6)%7)
	}
	step := func(value time.Time) time.Time { return value.AddDate(0, 0, 1) }
	if query.Interval == "week" {
		step = func(value time.Time) time.Time { return value.AddDate(0, 0, 7) }
	}
	points := []AnalyticsPoint{}
	for start.Before(query.To.UTC()) {
		points = append(points, AnalyticsPoint{StartsAt: start})
		start = step(start)
	}
	return points
}

func queryLineAnalyticsSeries(
	ctx context.Context,
	tx *sql.Tx,
	query AnalyticsQuery,
	lineIDs []string,
	indexes map[string]int,
	results []LineAnalytics,
) error {
	rows, err := tx.QueryContext(ctx, `
		WITH associated AS (
			SELECT DISTINCT line.route_id, membership.service_alert_id, membership.episode_number
			FROM alert_revision_episode_membership membership
			JOIN alert_revision_lines line ON line.alert_revision_id = membership.alert_revision_id
			WHERE line.route_id = ANY($3)
		), categorized AS (
			SELECT associated.route_id, category.first_seen_at AS category_first_seen_at,
				episode.first_seen_at AS observed_first_seen_at,
				episode.last_seen_at, episode.closed_at
			FROM associated
			JOIN alert_episodes episode USING (service_alert_id, episode_number)
			JOIN LATERAL (
				SELECT revision.first_seen_at
				FROM alert_revision_episode_membership membership
				JOIN service_alert_revisions revision ON revision.id = membership.alert_revision_id
				JOIN alert_revision_lines line ON line.alert_revision_id = revision.id
				WHERE membership.service_alert_id = associated.service_alert_id
					AND membership.episode_number = associated.episode_number
					AND line.route_id = associated.route_id AND NOT revision.is_deleted
				ORDER BY revision.first_seen_at, revision.revision_number, revision.id
				LIMIT 1
			) category ON true
		)
		SELECT route_id,
			date_trunc($4, category_first_seen_at AT TIME ZONE 'UTC') AT TIME ZONE 'UTC' AS starts_at,
			count(*), count(*) FILTER (WHERE closed_at IS NOT NULL),
			percentile_cont(0.5) WITHIN GROUP (
				ORDER BY extract(epoch FROM (last_seen_at - observed_first_seen_at))
			) FILTER (WHERE closed_at IS NOT NULL)
		FROM categorized
		WHERE category_first_seen_at >= $1 AND category_first_seen_at < $2
		GROUP BY route_id, starts_at
		ORDER BY route_id, starts_at`, query.From.UTC(), query.To.UTC(), lineIDs, query.Interval)
	if err != nil {
		return fmt.Errorf("query line analytics series: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var lineID string
		var point AnalyticsPoint
		var median sql.NullFloat64
		if err := rows.Scan(&lineID, &point.StartsAt, &point.AlertCount,
			&point.CompletedEpisodeSampleCount, &median); err != nil {
			return fmt.Errorf("scan line analytics series: %w", err)
		}
		point.MedianObservedLifetimeSeconds = nullFloatPointer(median)
		result := &results[indexes[lineID]]
		for index := range result.Series {
			if result.Series[index].StartsAt.Equal(point.StartsAt) {
				result.Series[index] = point
				break
			}
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate line analytics series: %w", err)
	}
	return nil
}

func queryLineAnalyticsBreakdown(
	ctx context.Context,
	tx *sql.Tx,
	query AnalyticsQuery,
	lineIDs []string,
	indexes map[string]int,
	results []LineAnalytics,
	category string,
) error {
	rows, err := tx.QueryContext(ctx, `
		WITH associated AS (
			SELECT DISTINCT line.route_id, membership.service_alert_id, membership.episode_number
			FROM alert_revision_episode_membership membership
			JOIN alert_revision_lines line ON line.alert_revision_id = membership.alert_revision_id
			WHERE line.route_id = ANY($3)
		), categorized AS (
			SELECT associated.route_id, category.first_seen_at, category.value
			FROM associated
			JOIN LATERAL (
				SELECT revision.first_seen_at,
					CASE WHEN $4 = 'cause' THEN revision.cause ELSE revision.effect END AS value
				FROM alert_revision_episode_membership membership
				JOIN service_alert_revisions revision ON revision.id = membership.alert_revision_id
				JOIN alert_revision_lines line ON line.alert_revision_id = revision.id
				WHERE membership.service_alert_id = associated.service_alert_id
					AND membership.episode_number = associated.episode_number
					AND line.route_id = associated.route_id AND NOT revision.is_deleted
				ORDER BY revision.first_seen_at, revision.revision_number, revision.id
				LIMIT 1
			) category ON true
		)
		SELECT route_id, COALESCE(value, 'UNSPECIFIED'), count(*)
		FROM categorized
		WHERE first_seen_at >= $1 AND first_seen_at < $2
		GROUP BY route_id, value
		ORDER BY route_id, count(*) DESC, COALESCE(value, 'UNSPECIFIED')`, query.From.UTC(), query.To.UTC(), lineIDs, category)
	if err != nil {
		return fmt.Errorf("query line analytics %s breakdown: %w", category, err)
	}
	defer rows.Close()
	for rows.Next() {
		var lineID string
		var breakdown AnalyticsBreakdown
		if err := rows.Scan(&lineID, &breakdown.Value, &breakdown.Count); err != nil {
			return fmt.Errorf("scan line analytics %s breakdown: %w", category, err)
		}
		result := &results[indexes[lineID]]
		if category == "cause" {
			result.Causes = append(result.Causes, breakdown)
		} else {
			result.Effects = append(result.Effects, breakdown)
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate line analytics %s breakdown: %w", category, err)
	}
	return nil
}

func (r *ReadRepository) GetLineAnalytics(ctx context.Context, lineID string, query AnalyticsQuery) (LineAnalytics, error) {
	tx, err := r.readTx(ctx, "line analytics detail query")
	if err != nil {
		return LineAnalytics{}, err
	}
	defer tx.Rollback()
	result, err := queryLineAnalytics(ctx, tx, lineID, query, true)
	if err != nil {
		return LineAnalytics{}, err
	}
	if err := commitReadTx(tx, "line analytics detail query"); err != nil {
		return LineAnalytics{}, err
	}
	return result, nil
}

func queryLineAnalytics(ctx context.Context, tx *sql.Tx, lineID string, query AnalyticsQuery, detailed bool) (LineAnalytics, error) {
	lines, err := queryLines(ctx, tx, true, query.Now.UTC(), lineID)
	if err != nil {
		return LineAnalytics{}, err
	}
	if len(lines) == 0 {
		return LineAnalytics{}, fmt.Errorf("line %q: %w", lineID, ErrNotFound)
	}
	result := LineAnalytics{
		Line:              lines[0],
		Series:            []AnalyticsPoint{},
		Causes:            []AnalyticsBreakdown{},
		Effects:           []AnalyticsBreakdown{},
		MetricLimitations: []string{},
	}
	if detailed {
		result.MetricLimitations = append(result.MetricLimitations, analyticsMetricLimitations...)
	}
	step := "1 day"
	if query.Interval == "week" {
		step = "1 week"
	}
	rows, err := tx.QueryContext(ctx, `
		WITH associated AS (
			SELECT DISTINCT membership.service_alert_id, membership.episode_number
			FROM alert_revision_episode_membership membership
			JOIN alert_revision_lines line ON line.alert_revision_id = membership.alert_revision_id
			WHERE line.route_id = $1
		), categorized AS (
			SELECT associated.service_alert_id, associated.episode_number,
				category.first_seen_at AS category_first_seen_at,
				episode.first_seen_at AS observed_first_seen_at,
				category.cause, category.effect,
				episode.last_seen_at, episode.closed_at
			FROM associated
			JOIN alert_episodes episode USING (service_alert_id, episode_number)
			JOIN LATERAL (
				SELECT revision.first_seen_at, revision.cause, revision.effect
				FROM alert_revision_episode_membership membership
				JOIN service_alert_revisions revision ON revision.id = membership.alert_revision_id
				JOIN alert_revision_lines line ON line.alert_revision_id = revision.id
				WHERE membership.service_alert_id = associated.service_alert_id
					AND membership.episode_number = associated.episode_number
					AND line.route_id = $1 AND NOT revision.is_deleted
				ORDER BY revision.first_seen_at, revision.revision_number, revision.id
				LIMIT 1
			) category ON true
		), buckets AS (
			SELECT generate_series(
				date_trunc($4, $2::timestamptz AT TIME ZONE 'UTC') AT TIME ZONE 'UTC',
				date_trunc($4, ($3::timestamptz - interval '1 microsecond') AT TIME ZONE 'UTC') AT TIME ZONE 'UTC',
				$5::interval
			) AS starts_at
		), aggregates AS (
			SELECT date_trunc($4, category_first_seen_at AT TIME ZONE 'UTC') AT TIME ZONE 'UTC' AS starts_at,
				count(*) AS alert_count,
				count(*) FILTER (WHERE closed_at IS NOT NULL) AS completed_count,
				percentile_cont(0.5) WITHIN GROUP (
					ORDER BY extract(epoch FROM (last_seen_at - observed_first_seen_at))
				) FILTER (WHERE closed_at IS NOT NULL) AS median_lifetime
			FROM categorized
			WHERE category_first_seen_at >= $2 AND category_first_seen_at < $3
			GROUP BY 1
		)
		SELECT bucket.starts_at, COALESCE(aggregate.alert_count, 0),
			COALESCE(aggregate.completed_count, 0), aggregate.median_lifetime
		FROM buckets bucket
		LEFT JOIN aggregates aggregate USING (starts_at)
		ORDER BY bucket.starts_at`, lineID, query.From.UTC(), query.To.UTC(), query.Interval, step)
	if err != nil {
		return LineAnalytics{}, fmt.Errorf("query analytics series for line %q: %w", lineID, err)
	}
	for rows.Next() {
		var point AnalyticsPoint
		var median sql.NullFloat64
		if err := rows.Scan(&point.StartsAt, &point.AlertCount,
			&point.CompletedEpisodeSampleCount, &median); err != nil {
			rows.Close()
			return LineAnalytics{}, fmt.Errorf("scan analytics point: %w", err)
		}
		point.MedianObservedLifetimeSeconds = nullFloatPointer(median)
		result.Series = append(result.Series, point)
	}
	if err := rows.Close(); err != nil {
		return LineAnalytics{}, fmt.Errorf("close analytics series: %w", err)
	}
	for category, destination := range map[string]*[]AnalyticsBreakdown{
		"cause": &result.Causes, "effect": &result.Effects,
	} {
		breakdownRows, err := tx.QueryContext(ctx, `
			WITH associated AS (
				SELECT DISTINCT membership.service_alert_id, membership.episode_number
				FROM alert_revision_episode_membership membership
				JOIN alert_revision_lines line ON line.alert_revision_id = membership.alert_revision_id
				WHERE line.route_id = $1
			), categorized AS (
				SELECT category.first_seen_at, category.value
				FROM associated
				JOIN LATERAL (
					SELECT revision.first_seen_at, CASE WHEN $4 = 'cause' THEN revision.cause ELSE revision.effect END AS value
					FROM alert_revision_episode_membership membership
					JOIN service_alert_revisions revision ON revision.id = membership.alert_revision_id
					JOIN alert_revision_lines line ON line.alert_revision_id = revision.id
					WHERE membership.service_alert_id = associated.service_alert_id
						AND membership.episode_number = associated.episode_number
						AND line.route_id = $1 AND NOT revision.is_deleted
					ORDER BY revision.first_seen_at, revision.revision_number, revision.id
					LIMIT 1
				) category ON true
			)
			SELECT COALESCE(value, 'UNSPECIFIED'), count(*)
			FROM categorized
			WHERE first_seen_at >= $2 AND first_seen_at < $3
			GROUP BY 1
			ORDER BY count(*) DESC, COALESCE(value, 'UNSPECIFIED')`, lineID, query.From.UTC(), query.To.UTC(), category)
		if err != nil {
			return LineAnalytics{}, fmt.Errorf("query analytics %s breakdown for line %q: %w", category, lineID, err)
		}
		for breakdownRows.Next() {
			var breakdown AnalyticsBreakdown
			if err := breakdownRows.Scan(&breakdown.Value, &breakdown.Count); err != nil {
				breakdownRows.Close()
				return LineAnalytics{}, fmt.Errorf("scan analytics %s breakdown: %w", category, err)
			}
			*destination = append(*destination, breakdown)
		}
		if err := breakdownRows.Close(); err != nil {
			return LineAnalytics{}, fmt.Errorf("close analytics %s breakdown: %w", category, err)
		}
	}
	return result, nil
}
