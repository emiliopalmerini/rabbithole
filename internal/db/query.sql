-- name: CreateSession :one
INSERT INTO sessions (exchange, routing_key, queue_name, amqp_url)
VALUES (?, ?, ?, ?)
RETURNING id;

-- name: EndSession :exec
UPDATE sessions SET ended_at = CURRENT_TIMESTAMP WHERE id = ?;

-- name: ListRecentSessions :many
SELECT id, started_at, ended_at, exchange, routing_key, queue_name, amqp_url
FROM sessions
ORDER BY started_at DESC
LIMIT ?;

-- name: InsertMessage :one
INSERT INTO messages (
    session_id, exchange, routing_key, body, content_type,
    headers, timestamp, proto_type, correlation_id, reply_to,
    message_id, app_id
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
RETURNING id;

-- name: GetMessage :one
SELECT id, session_id, exchange, routing_key, body, content_type,
       headers, timestamp, consumed_at, proto_type, correlation_id,
       reply_to, message_id, app_id
FROM messages
WHERE id = ?;

-- name: ListMessagesBySession :many
SELECT id, session_id, exchange, routing_key, body, content_type,
       headers, timestamp, consumed_at, proto_type, correlation_id,
       reply_to, message_id, app_id
FROM messages
WHERE session_id = ?
ORDER BY consumed_at DESC
LIMIT ? OFFSET ?;

