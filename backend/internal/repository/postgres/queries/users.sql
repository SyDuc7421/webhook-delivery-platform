-- name: CreateOrg :exec
INSERT INTO orgs (id, name) VALUES ($1, $2);

-- name: GetOrg :one
SELECT id, name, created_at FROM orgs WHERE id = $1;

-- name: CreateUser :exec
INSERT INTO users (id, org_id, email, password_hash, role) VALUES ($1, $2, $3, $4, $5);

-- name: GetUserByEmail :one
SELECT id, org_id, email, password_hash, role, created_at FROM users WHERE email = $1;

-- name: GetUser :one
SELECT id, org_id, email, password_hash, role, created_at FROM users WHERE id = $1;
