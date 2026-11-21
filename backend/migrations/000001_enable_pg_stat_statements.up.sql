-- Query statistics for profiling hot paths (docs/DATABASE.md). Requires
-- shared_preload_libraries=pg_stat_statements (set in deploy/docker-compose.yml; RDS default).
CREATE EXTENSION IF NOT EXISTS pg_stat_statements;
