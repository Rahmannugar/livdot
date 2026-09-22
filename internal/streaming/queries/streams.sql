-- name: CreateEventStream :one
INSERT INTO event_streams (id, event_id, livekit_room_id, started_at)
VALUES ($1, $2, $3, $4)
RETURNING id, event_id, livekit_room_id, viewer_count, status,
          started_at, finished_at, failed_at, failure_reason;

-- name: GetEventStreamByEvent :one
SELECT id, event_id, livekit_room_id, viewer_count, status,
       started_at, finished_at, failed_at, failure_reason
FROM event_streams
WHERE event_id = $1;

-- name: RecordStreamFailure :one
UPDATE event_streams
SET status = 'failed',
    finished_at = COALESCE(finished_at, $2),
    failed_at = COALESCE(failed_at, $2),
    failure_reason = $3
WHERE id = $1
  AND status = 'live'
RETURNING id, event_id, livekit_room_id, viewer_count, status,
          started_at, finished_at, failed_at, failure_reason;

-- name: RecordStreamEnd :one
UPDATE event_streams
SET status = 'ended',
    finished_at = COALESCE(finished_at, $2)
WHERE id = $1
  AND status = 'live'
RETURNING id, event_id, livekit_room_id, viewer_count, status,
          started_at, finished_at, failed_at, failure_reason;

-- name: RecordStreamMemberJoin :one
INSERT INTO event_stream_members (id, stream_id, event_member_id, joined_at)
VALUES ($1, $2, $3, $4)
ON CONFLICT (stream_id, event_member_id)
DO UPDATE SET left_at = NULL
RETURNING id, stream_id, event_member_id, joined_at, left_at;
