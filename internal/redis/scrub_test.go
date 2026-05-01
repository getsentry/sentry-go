package redis

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestScrubCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		cmds           []string
		sendDefaultPII bool
		want           string
	}{
		{name: "empty", cmds: nil, want: ""},
		{name: "GET single key", cmds: []string{"GET", "mykey"}, want: "GET mykey"},
		{name: "SET key value", cmds: []string{"SET", "mykey", "secretvalue"}, want: "SET mykey ?"},
		{name: "MSET multiple key-value pairs", cmds: []string{"MSET", "k1", "v1", "k2", "v2", "k3", "v3"}, want: "MSET k1 ? k2 ? k3 ?"},
		{name: "MGET multiple keys", cmds: []string{"MGET", "k1", "k2", "k3"}, want: "MGET k1 k2 k3"},
		{name: "LPUSH list with values", cmds: []string{"LPUSH", "mylist", "a", "b", "c"}, want: "LPUSH mylist ? ? ?"},
		{name: "SADD set with members", cmds: []string{"SADD", "myset", "member1", "member2"}, want: "SADD myset ? ?"},
		{name: "DEL multiple keys", cmds: []string{"DEL", "k1", "k2"}, want: "DEL k1 k2"},
		{name: "SUBSCRIBE channels", cmds: []string{"SUBSCRIBE", "ch1", "ch2"}, want: "SUBSCRIBE ch1 ch2"},
		{name: "PUBLISH channel message", cmds: []string{"PUBLISH", "mychannel", "hello"}, want: "PUBLISH mychannel ?"},
		{name: "SETNX key value", cmds: []string{"SETNX", "mykey", "value"}, want: "SETNX mykey ?"},
		{name: "INCR no value to scrub", cmds: []string{"INCR", "counter"}, want: "INCR counter"},
		{name: "PING no args", cmds: []string{"PING"}, want: "PING"},
		{name: "RENAME two keys", cmds: []string{"RENAME", "oldkey", "newkey"}, want: "RENAME oldkey newkey"},
		{name: "ZADD scrubs all params", cmds: []string{"ZADD", "myzset", "100", "user@email.com"}, want: "ZADD myzset ? ?"},
		{name: "ZADD multiple score-member pairs", cmds: []string{"ZADD", "myzset", "1", "alice", "2", "bob"}, want: "ZADD myzset ? ? ? ?"},
		{name: "ZADD with NX flag", cmds: []string{"ZADD", "myzset", "NX", "100", "user@email.com"}, want: "ZADD myzset ? ? ?"},
		{name: "unknown command scrubs all args", cmds: []string{"CUSTOMCMD", "arg1", "arg2", "arg3"}, want: "CUSTOMCMD ? ? ?"},
		{name: "unknown command single arg", cmds: []string{"XYZZY", "foo"}, want: "XYZZY ?"},

		// Commands where sendDefaultPII=false scrubs fields
		{name: "SET with EX scrubs flags", cmds: []string{"SET", "mykey", "secretvalue", "EX", "60"}, want: "SET mykey ? ? ?"},
		{name: "SET with NX scrubs flag", cmds: []string{"SET", "mykey", "secretvalue", "NX"}, want: "SET mykey ? ?"},
		{name: "HSET scrubs fields and values", cmds: []string{"HSET", "myhash", "field1", "val1", "field2", "val2"}, want: "HSET myhash ? ? ? ?"},
		{name: "HGET scrubs field", cmds: []string{"HGET", "myhash", "field1"}, want: "HGET myhash ?"},
		{name: "HMGET scrubs fields", cmds: []string{"HMGET", "myhash", "f1", "f2", "f3"}, want: "HMGET myhash ? ? ?"},
		{name: "LRANGE scrubs indices", cmds: []string{"LRANGE", "mylist", "0", "-1"}, want: "LRANGE mylist ? ?"},
		{name: "EXPIRE scrubs seconds", cmds: []string{"EXPIRE", "mykey", "300"}, want: "EXPIRE mykey ?"},
		{name: "SETEX scrubs seconds and value", cmds: []string{"SETEX", "mykey", "60", "secretvalue"}, want: "SETEX mykey ? ?"},
		{name: "LSET scrubs index and value", cmds: []string{"LSET", "mylist", "0", "newvalue"}, want: "LSET mylist ? ?"},
		{name: "ZINCRBY scrubs increment and member", cmds: []string{"ZINCRBY", "myzset", "2", "member1"}, want: "ZINCRBY myzset ? ?"},
		{name: "HSETNX scrubs field and value", cmds: []string{"HSETNX", "myhash", "field1", "value1"}, want: "HSETNX myhash ? ?"},

		// Same commands with sendDefaultPII=true preserve fields
		{name: "PII SET with EX preserves flags", cmds: []string{"SET", "mykey", "secretvalue", "EX", "60"}, sendDefaultPII: true, want: "SET mykey ? EX 60"},
		{name: "PII HSET preserves field names", cmds: []string{"HSET", "myhash", "field1", "val1", "field2", "val2"}, sendDefaultPII: true, want: "HSET myhash field1 ? field2 ?"},
		{name: "PII HGET preserves field name", cmds: []string{"HGET", "myhash", "field1"}, sendDefaultPII: true, want: "HGET myhash field1"},
		{name: "PII HMGET preserves field names", cmds: []string{"HMGET", "myhash", "f1", "f2", "f3"}, sendDefaultPII: true, want: "HMGET myhash f1 f2 f3"},
		{name: "PII LRANGE preserves indices", cmds: []string{"LRANGE", "mylist", "0", "-1"}, sendDefaultPII: true, want: "LRANGE mylist 0 -1"},
		{name: "PII EXPIRE preserves seconds", cmds: []string{"EXPIRE", "mykey", "300"}, sendDefaultPII: true, want: "EXPIRE mykey 300"},
		{name: "PII SETEX preserves seconds", cmds: []string{"SETEX", "mykey", "60", "secretvalue"}, sendDefaultPII: true, want: "SETEX mykey 60 ?"},
		{name: "PII HSETNX preserves field", cmds: []string{"HSETNX", "myhash", "field1", "value1"}, sendDefaultPII: true, want: "HSETNX myhash field1 ?"},
		{name: "PII ZINCRBY preserves increment", cmds: []string{"ZINCRBY", "myzset", "2", "member1"}, sendDefaultPII: true, want: "ZINCRBY myzset 2 ?"},

		// Sensitive commands always scrub, even with sendDefaultPII=true
		{name: "AUTH scrubs password", cmds: []string{"AUTH", "supersecret"}, want: "AUTH ?"},
		{name: "AUTH scrubs username and password", cmds: []string{"AUTH", "admin", "supersecret"}, want: "AUTH ? ?"},
		{name: "ECHO scrubs value", cmds: []string{"ECHO", "sensitive-data"}, want: "ECHO ?"},
		{name: "PII AUTH still scrubs password", cmds: []string{"AUTH", "supersecret"}, sendDefaultPII: true, want: "AUTH ?"},
		{name: "PII AUTH still scrubs username and password", cmds: []string{"AUTH", "admin", "supersecret"}, sendDefaultPII: true, want: "AUTH ? ?"},
		{name: "PII ECHO still scrubs value", cmds: []string{"ECHO", "sensitive-data"}, sendDefaultPII: true, want: "ECHO ?"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, ScrubCommand(tt.cmds, tt.sendDefaultPII))
		})
	}
}

func TestExtractKeys(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cmds []string
		want []string
	}{
		{name: "nil input", cmds: nil, want: nil},
		{name: "command only", cmds: []string{"PING"}, want: nil},
		{name: "GET single key", cmds: []string{"GET", "mykey"}, want: []string{"mykey"}},
		{name: "SET key value", cmds: []string{"SET", "mykey", "val"}, want: []string{"mykey"}},
		{name: "MGET multiple keys", cmds: []string{"MGET", "k1", "k2", "k3"}, want: []string{"k1", "k2", "k3"}},
		{name: "MSET alternating keys and values", cmds: []string{"MSET", "k1", "v1", "k2", "v2"}, want: []string{"k1", "k2"}},
		{name: "DEL multiple keys", cmds: []string{"DEL", "k1", "k2", "k3"}, want: []string{"k1", "k2", "k3"}},
		{name: "HSET returns only the hash key not fields", cmds: []string{"HSET", "myhash", "field1", "val1", "field2", "val2"}, want: []string{"myhash"}},
		{name: "HGET returns only the hash key not fields", cmds: []string{"HGET", "myhash", "field1"}, want: []string{"myhash"}},
		{name: "RENAME both keys", cmds: []string{"RENAME", "oldkey", "newkey"}, want: []string{"oldkey", "newkey"}},
		{name: "unknown command returns first arg", cmds: []string{"CUSTOMCMD", "arg1", "arg2"}, want: []string{"arg1"}},
		{name: "SUBSCRIBE channels as keys", cmds: []string{"SUBSCRIBE", "ch1", "ch2"}, want: []string{"ch1", "ch2"}},
		{name: "AUTH does not leak password", cmds: []string{"AUTH", "supersecret"}, want: nil},
		{name: "AUTH does not leak username or password", cmds: []string{"AUTH", "admin", "supersecret"}, want: nil},
		{name: "ECHO does not leak value", cmds: []string{"ECHO", "sensitive-data"}, want: nil},
		{name: "SELECT does not leak db index as key", cmds: []string{"SELECT", "3"}, want: nil},
		{name: "PING with no args returns nil", cmds: []string{"PING"}, want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, ExtractKeys(tt.cmds))
		})
	}
}

func TestCommandName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cmds []string
		want string
	}{
		{name: "normal command", cmds: []string{"get", "key"}, want: "GET"},
		{name: "already uppercase", cmds: []string{"SET", "key", "val"}, want: "SET"},
		{name: "empty slice", cmds: nil, want: "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, CommandName(tt.cmds))
		})
	}
}

func TestIsDeleteCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cmds []string
		want bool
	}{
		{name: "DEL", cmds: []string{"DEL", "key"}, want: true},
		{name: "UNLINK", cmds: []string{"UNLINK", "key"}, want: true},
		{name: "GETDEL", cmds: []string{"GETDEL", "key"}, want: true},
		{name: "GET is not delete", cmds: []string{"GET", "key"}, want: false},
		{name: "SET is not delete", cmds: []string{"SET", "key", "val"}, want: false},
		{name: "empty", cmds: nil, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, IsDeleteCommand(tt.cmds))
		})
	}
}