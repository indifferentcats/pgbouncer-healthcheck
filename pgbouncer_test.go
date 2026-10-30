package main

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

// Column lists returned by PGBouncer 1.25 (psql -c "SHOW ..."). A mistyped db
// tag would otherwise silently leave a field at its zero value, because
// queryRows ignores unknown columns.
var pgbouncerColumns = map[string]struct {
	typ     reflect.Type
	columns string
}{
	"USERS":     {reflect.TypeFor[User](), "name|pool_size|reserve_pool_size|pool_mode|max_user_connections|current_connections|max_user_client_connections|current_client_connections"},
	"CONFIG":    {reflect.TypeFor[Config](), "key|value|default|changeable"},
	"DATABASES": {reflect.TypeFor[Database](), "name|host|port|database|force_user|pool_size|min_pool_size|reserve_pool_size|server_lifetime|pool_mode|load_balance_hosts|max_connections|current_connections|max_client_connections|current_client_connections|paused|disabled"},
	"POOLS":     {reflect.TypeFor[Pool](), "database|user|cl_active|cl_waiting|cl_active_cancel_req|cl_waiting_cancel_req|sv_active|sv_active_cancel|sv_being_canceled|sv_idle|sv_used|sv_tested|sv_login|maxwait|maxwait_us|pool_mode|load_balance_hosts"},
	"CLIENTS":   {reflect.TypeFor[Connection](), "type|user|database|replication|state|addr|port|local_addr|local_port|connect_time|request_time|wait|wait_us|close_needed|ptr|link|remote_pid|tls|application_name|prepared_statements|id"},
	"MEM":       {reflect.TypeFor[Mem](), "name|size|used|free|memtotal"},
	"STATS":     {reflect.TypeFor[Stat](), "database|total_server_assignment_count|total_xact_count|total_query_count|total_received|total_sent|total_xact_time|total_query_time|total_wait_time|total_client_parse_count|total_server_parse_count|total_bind_count|avg_server_assignment_count|avg_xact_count|avg_query_count|avg_recv|avg_sent|avg_xact_time|avg_query_time|avg_wait_time|avg_client_parse_count|avg_server_parse_count|avg_bind_count"},
}

func TestStructTagsMatchPGBouncerColumns(t *testing.T) {
	for cmd, want := range pgbouncerColumns {
		t.Run(cmd, func(t *testing.T) {
			var tags []string
			for f := range want.typ.Fields() {
				dbTag := f.Tag.Get("db")
				if jsonTag := f.Tag.Get("json"); jsonTag != dbTag {
					t.Errorf("%s.%s: json tag %q != db tag %q", want.typ.Name(), f.Name, jsonTag, dbTag)
				}
				tags = append(tags, dbTag)
			}
			columns := strings.Split(want.columns, "|")
			if !slices.Equal(tags, columns) {
				t.Errorf("%s db tags:\n got %v\nwant %v", want.typ.Name(), tags, columns)
			}
		})
	}
}
