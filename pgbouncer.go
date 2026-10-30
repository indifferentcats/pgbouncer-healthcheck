package main

import (
	"context"
	"database/sql"
)

// The types below mirror PGBouncer's admin console SHOW result sets (as of
// PGBouncer 1.25). Rows are scanned by column name (see queryRows), so
// columns added or removed by other PGBouncer versions are tolerated: unknown
// columns are ignored and missing ones are left at their zero value.
//
// JSON field names are the PGBouncer column names. Pointer fields are columns
// PGBouncer may report as NULL; they serialise as null.

// User as returned by PGBouncer's SHOW USERS
type User struct {
	Name                     string      `db:"name" json:"name"`
	PoolSize                 OptionalInt `db:"pool_size" json:"pool_size"`
	ReservePoolSize          OptionalInt `db:"reserve_pool_size" json:"reserve_pool_size"`
	PoolMode                 *string     `db:"pool_mode" json:"pool_mode"`
	MaxUserConnections       int64       `db:"max_user_connections" json:"max_user_connections"`
	CurrentConnections       int64       `db:"current_connections" json:"current_connections"`
	MaxUserClientConnections int64       `db:"max_user_client_connections" json:"max_user_client_connections"`
	CurrentClientConnections int64       `db:"current_client_connections" json:"current_client_connections"`
}

// Config item as returned by PGBouncer's SHOW CONFIG
type Config struct {
	Key        string  `db:"key" json:"key"`
	Value      *string `db:"value" json:"value"`
	Default    *string `db:"default" json:"default"`
	Changeable YesNo   `db:"changeable" json:"changeable"`
}

// Database as returned by PGBouncer's SHOW DATABASES
type Database struct {
	Name                     string  `db:"name" json:"name"`
	Host                     *string `db:"host" json:"host"`
	Port                     int64   `db:"port" json:"port"`
	Database                 string  `db:"database" json:"database"`
	ForceUser                *string `db:"force_user" json:"force_user"`
	PoolSize                 int64   `db:"pool_size" json:"pool_size"`
	MinPoolSize              int64   `db:"min_pool_size" json:"min_pool_size"`
	ReservePoolSize          int64   `db:"reserve_pool_size" json:"reserve_pool_size"`
	ServerLifetime           int64   `db:"server_lifetime" json:"server_lifetime"`
	PoolMode                 *string `db:"pool_mode" json:"pool_mode"`
	LoadBalanceHosts         *string `db:"load_balance_hosts" json:"load_balance_hosts"`
	MaxConnections           int64   `db:"max_connections" json:"max_connections"`
	CurrentConnections       int64   `db:"current_connections" json:"current_connections"`
	MaxClientConnections     int64   `db:"max_client_connections" json:"max_client_connections"`
	CurrentClientConnections int64   `db:"current_client_connections" json:"current_client_connections"`
	Paused                   int64   `db:"paused" json:"paused"`
	Disabled                 int64   `db:"disabled" json:"disabled"`
}

// Pool as returned by PGBouncer's SHOW POOLS
type Pool struct {
	Database           string  `db:"database" json:"database"`
	User               string  `db:"user" json:"user"`
	ClActive           int64   `db:"cl_active" json:"cl_active"`
	ClWaiting          int64   `db:"cl_waiting" json:"cl_waiting"`
	ClActiveCancelReq  int64   `db:"cl_active_cancel_req" json:"cl_active_cancel_req"`
	ClWaitingCancelReq int64   `db:"cl_waiting_cancel_req" json:"cl_waiting_cancel_req"`
	SvActive           int64   `db:"sv_active" json:"sv_active"`
	SvActiveCancel     int64   `db:"sv_active_cancel" json:"sv_active_cancel"`
	SvBeingCanceled    int64   `db:"sv_being_canceled" json:"sv_being_canceled"`
	SvIdle             int64   `db:"sv_idle" json:"sv_idle"`
	SvUsed             int64   `db:"sv_used" json:"sv_used"`
	SvTested           int64   `db:"sv_tested" json:"sv_tested"`
	SvLogin            int64   `db:"sv_login" json:"sv_login"`
	MaxWait            int64   `db:"maxwait" json:"maxwait"`
	MaxWaitUs          int64   `db:"maxwait_us" json:"maxwait_us"`
	PoolMode           *string `db:"pool_mode" json:"pool_mode"`
	LoadBalanceHosts   *string `db:"load_balance_hosts" json:"load_balance_hosts"`
}

// Connection as returned by PGBouncer's SHOW CLIENTS and SHOW SERVERS,
// which share the same columns.
type Connection struct {
	Type               string `db:"type" json:"type"`
	User               string `db:"user" json:"user"`
	Database           string `db:"database" json:"database"`
	Replication        string `db:"replication" json:"replication"`
	State              string `db:"state" json:"state"`
	Addr               string `db:"addr" json:"addr"`
	Port               int64  `db:"port" json:"port"`
	LocalAddr          string `db:"local_addr" json:"local_addr"`
	LocalPort          int64  `db:"local_port" json:"local_port"`
	ConnectTime        string `db:"connect_time" json:"connect_time"`
	RequestTime        string `db:"request_time" json:"request_time"`
	Wait               int64  `db:"wait" json:"wait"`
	WaitUs             int64  `db:"wait_us" json:"wait_us"`
	CloseNeeded        int64  `db:"close_needed" json:"close_needed"`
	Ptr                string `db:"ptr" json:"ptr"`
	Link               string `db:"link" json:"link"`
	RemotePid          int64  `db:"remote_pid" json:"remote_pid"`
	TLS                string `db:"tls" json:"tls"`
	ApplicationName    string `db:"application_name" json:"application_name"`
	PreparedStatements int64  `db:"prepared_statements" json:"prepared_statements"`
	ID                 int64  `db:"id" json:"id"`
}

// Mem info record as returned by PGBouncer's SHOW MEM
type Mem struct {
	Name     string `db:"name" json:"name"`
	Size     int64  `db:"size" json:"size"`
	Used     int64  `db:"used" json:"used"`
	Free     int64  `db:"free" json:"free"`
	MemTotal int64  `db:"memtotal" json:"memtotal"`
}

// Stat record as returned by PGBouncer's SHOW STATS
type Stat struct {
	Database                   string `db:"database" json:"database"`
	TotalServerAssignmentCount int64  `db:"total_server_assignment_count" json:"total_server_assignment_count"`
	TotalXactCount             int64  `db:"total_xact_count" json:"total_xact_count"`
	TotalQueryCount            int64  `db:"total_query_count" json:"total_query_count"`
	TotalReceived              int64  `db:"total_received" json:"total_received"`
	TotalSent                  int64  `db:"total_sent" json:"total_sent"`
	TotalXactTime              int64  `db:"total_xact_time" json:"total_xact_time"`
	TotalQueryTime             int64  `db:"total_query_time" json:"total_query_time"`
	TotalWaitTime              int64  `db:"total_wait_time" json:"total_wait_time"`
	TotalClientParseCount      int64  `db:"total_client_parse_count" json:"total_client_parse_count"`
	TotalServerParseCount      int64  `db:"total_server_parse_count" json:"total_server_parse_count"`
	TotalBindCount             int64  `db:"total_bind_count" json:"total_bind_count"`
	AvgServerAssignmentCount   int64  `db:"avg_server_assignment_count" json:"avg_server_assignment_count"`
	AvgXactCount               int64  `db:"avg_xact_count" json:"avg_xact_count"`
	AvgQueryCount              int64  `db:"avg_query_count" json:"avg_query_count"`
	AvgRecv                    int64  `db:"avg_recv" json:"avg_recv"`
	AvgSent                    int64  `db:"avg_sent" json:"avg_sent"`
	AvgXactTime                int64  `db:"avg_xact_time" json:"avg_xact_time"`
	AvgQueryTime               int64  `db:"avg_query_time" json:"avg_query_time"`
	AvgWaitTime                int64  `db:"avg_wait_time" json:"avg_wait_time"`
	AvgClientParseCount        int64  `db:"avg_client_parse_count" json:"avg_client_parse_count"`
	AvgServerParseCount        int64  `db:"avg_server_parse_count" json:"avg_server_parse_count"`
	AvgBindCount               int64  `db:"avg_bind_count" json:"avg_bind_count"`
}

func getUsers(ctx context.Context, db *sql.DB) ([]User, error) {
	return queryRows[User](ctx, db, "SHOW USERS")
}

func getConfigs(ctx context.Context, db *sql.DB) ([]Config, error) {
	return queryRows[Config](ctx, db, "SHOW CONFIG")
}

func getDatabases(ctx context.Context, db *sql.DB) ([]Database, error) {
	return queryRows[Database](ctx, db, "SHOW DATABASES")
}

func getPools(ctx context.Context, db *sql.DB) ([]Pool, error) {
	return queryRows[Pool](ctx, db, "SHOW POOLS")
}

func getClients(ctx context.Context, db *sql.DB) ([]Connection, error) {
	return queryRows[Connection](ctx, db, "SHOW CLIENTS")
}

func getServers(ctx context.Context, db *sql.DB) ([]Connection, error) {
	return queryRows[Connection](ctx, db, "SHOW SERVERS")
}

func getMems(ctx context.Context, db *sql.DB) ([]Mem, error) {
	return queryRows[Mem](ctx, db, "SHOW MEM")
}

func getStats(ctx context.Context, db *sql.DB) ([]Stat, error) {
	return queryRows[Stat](ctx, db, "SHOW STATS")
}
