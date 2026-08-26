package store

var migrations = []string{
	`CREATE TABLE IF NOT EXISTS schema_migrations (
    version INTEGER PRIMARY KEY,
    applied_at TEXT NOT NULL
);
CREATE TABLE users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    display_name TEXT NOT NULL,
    role TEXT NOT NULL CHECK(role IN ('line_manager','operator','safety_officer','quality_engineer','maintenance_engineer','integrator')),
    active INTEGER NOT NULL DEFAULT 1 CHECK(active IN (0,1)),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE TABLE sessions (
    id TEXT PRIMARY KEY,
    user_id INTEGER NOT NULL REFERENCES users(id),
    token_hash TEXT NOT NULL UNIQUE,
    expires_at TEXT NOT NULL,
    revoked_at TEXT,
    last_seen_at TEXT NOT NULL,
    created_at TEXT NOT NULL
);
CREATE INDEX sessions_user_active_idx ON sessions(user_id, expires_at, revoked_at);
CREATE TABLE production_batches (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    code TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    status TEXT NOT NULL,
    starts_at TEXT NOT NULL,
    ends_at TEXT NOT NULL,
    version INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE TABLE workstations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    code TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    line TEXT NOT NULL,
    active INTEGER NOT NULL DEFAULT 1 CHECK(active IN (0,1)),
    version INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL
);
CREATE TABLE tools (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    code TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    calibration_due TEXT NOT NULL,
    active INTEGER NOT NULL DEFAULT 1 CHECK(active IN (0,1)),
    version INTEGER NOT NULL DEFAULT 1
);
CREATE TABLE robot_cells (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    code TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    batch_id INTEGER REFERENCES production_batches(id),
    workstation_id INTEGER NOT NULL REFERENCES workstations(id),
    integrator_id INTEGER NOT NULL REFERENCES users(id),
    status TEXT NOT NULL,
    safety_passed INTEGER NOT NULL DEFAULT 0 CHECK(safety_passed IN (0,1)),
    quality_passed INTEGER NOT NULL DEFAULT 0 CHECK(quality_passed IN (0,1)),
    calibration_ref TEXT NOT NULL DEFAULT '',
    version INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX robot_cells_status_idx ON robot_cells(status, workstation_id);
CREATE TABLE qualifications (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users(id),
    kind TEXT NOT NULL,
    valid_from TEXT NOT NULL,
    valid_to TEXT NOT NULL,
    revoked_at TEXT,
    UNIQUE(user_id, kind, valid_from)
);
CREATE INDEX qualifications_lookup_idx ON qualifications(user_id, kind, valid_to);
CREATE TABLE work_windows (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    cell_id INTEGER NOT NULL REFERENCES robot_cells(id),
    workstation_id INTEGER NOT NULL REFERENCES workstations(id),
    tool_id INTEGER NOT NULL REFERENCES tools(id),
    qualified_user_id INTEGER NOT NULL REFERENCES users(id),
    starts_at TEXT NOT NULL,
    ends_at TEXT NOT NULL,
    status TEXT NOT NULL,
    purpose TEXT NOT NULL,
    version INTEGER NOT NULL DEFAULT 1,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX work_windows_period_idx ON work_windows(starts_at, ends_at, status);
CREATE TABLE resource_reservations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    window_id INTEGER NOT NULL REFERENCES work_windows(id) ON DELETE CASCADE,
    resource_kind TEXT NOT NULL CHECK(resource_kind IN ('workstation','tool','person')),
    resource_id INTEGER NOT NULL,
    starts_at TEXT NOT NULL,
    ends_at TEXT NOT NULL,
    released_at TEXT,
    UNIQUE(window_id, resource_kind, resource_id)
);
CREATE INDEX reservations_conflict_idx ON resource_reservations(resource_kind, resource_id, starts_at, ends_at, released_at);
CREATE TABLE inspections (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    cell_id INTEGER NOT NULL REFERENCES robot_cells(id),
    kind TEXT NOT NULL CHECK(kind IN ('safety','quality')),
    passed INTEGER NOT NULL CHECK(passed IN (0,1)),
    inspector_id INTEGER NOT NULL REFERENCES users(id),
    notes TEXT NOT NULL,
    recorded_at TEXT NOT NULL
);
CREATE INDEX inspections_cell_idx ON inspections(cell_id, kind, recorded_at);
CREATE TABLE spare_parts (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    code TEXT NOT NULL UNIQUE,
    name TEXT NOT NULL,
    available INTEGER NOT NULL CHECK(available >= 0),
    reserved INTEGER NOT NULL DEFAULT 0 CHECK(reserved >= 0 AND reserved <= available),
    version INTEGER NOT NULL DEFAULT 1,
    updated_at TEXT NOT NULL
);
CREATE TABLE maintenance_orders (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    code TEXT NOT NULL UNIQUE,
    cell_id INTEGER NOT NULL REFERENCES robot_cells(id),
    assignee_id INTEGER NOT NULL REFERENCES users(id),
    spare_part_id INTEGER REFERENCES spare_parts(id),
    spare_quantity INTEGER NOT NULL DEFAULT 0 CHECK(spare_quantity >= 0),
    priority INTEGER NOT NULL CHECK(priority BETWEEN 1 AND 5),
    summary TEXT NOT NULL,
    status TEXT NOT NULL,
    version INTEGER NOT NULL DEFAULT 1,
    due_at TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX maintenance_cell_status_idx ON maintenance_orders(cell_id, status, due_at);
CREATE TABLE part_movements (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    part_id INTEGER NOT NULL REFERENCES spare_parts(id),
    order_id INTEGER NOT NULL REFERENCES maintenance_orders(id),
    quantity INTEGER NOT NULL,
    kind TEXT NOT NULL CHECK(kind IN ('reserve','consume','release')),
    created_at TEXT NOT NULL,
    UNIQUE(order_id, kind)
);
CREATE TABLE recovery_jobs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    kind TEXT NOT NULL,
    object_type TEXT NOT NULL,
    object_id INTEGER NOT NULL,
    idempotency_key TEXT NOT NULL UNIQUE,
    payload BLOB NOT NULL,
    status TEXT NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0,
    max_attempts INTEGER NOT NULL,
    next_attempt_at TEXT NOT NULL,
    lease_owner TEXT NOT NULL DEFAULT '',
    lease_until TEXT,
    last_error TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX recovery_claim_idx ON recovery_jobs(status, next_attempt_at, lease_until);
CREATE TABLE idempotency_records (
    scope TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    request_hash TEXT NOT NULL,
    response_status INTEGER NOT NULL,
    response_body BLOB NOT NULL,
    expires_at TEXT NOT NULL,
    created_at TEXT NOT NULL,
    PRIMARY KEY(scope, idempotency_key)
);
CREATE TABLE audit_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    actor_id INTEGER NOT NULL REFERENCES users(id),
    actor_role TEXT NOT NULL,
    action TEXT NOT NULL,
    object_type TEXT NOT NULL,
    object_id TEXT NOT NULL,
    result TEXT NOT NULL,
    request_id TEXT NOT NULL,
    details BLOB NOT NULL,
    occurred_at TEXT NOT NULL,
    previous_hash TEXT NOT NULL,
    event_hash TEXT NOT NULL UNIQUE
);
CREATE INDEX audit_object_idx ON audit_events(object_type, object_id, occurred_at);
CREATE TABLE cell_transitions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    cell_id INTEGER NOT NULL REFERENCES robot_cells(id),
    from_status TEXT NOT NULL,
    to_status TEXT NOT NULL,
    actor_id INTEGER NOT NULL REFERENCES users(id),
    reason TEXT NOT NULL,
    request_id TEXT NOT NULL,
    occurred_at TEXT NOT NULL
);`,
	`CREATE TRIGGER reservations_validate_insert
BEFORE INSERT ON resource_reservations
WHEN EXISTS (
    SELECT 1 FROM resource_reservations current
    WHERE current.resource_kind = NEW.resource_kind
      AND current.resource_id = NEW.resource_id
      AND current.released_at IS NULL
      AND NEW.starts_at < current.ends_at
      AND current.starts_at < NEW.ends_at
)
BEGIN
    SELECT RAISE(ABORT, 'resource reservation conflict');
END;
CREATE TRIGGER sessions_reject_expired_insert
BEFORE INSERT ON sessions
WHEN NEW.expires_at <= NEW.created_at
BEGIN
    SELECT RAISE(ABORT, 'session must expire after creation');
END;`,
}
