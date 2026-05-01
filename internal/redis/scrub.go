package redis

import "strings"

// argRole classifies a command argument position.
type argRole byte

const (
	roleKey   argRole = 'k' // Redis key -- preserved in descriptions and collected for cache.key
	roleField argRole = 'f' // Field name, index, flag, or metadata -- preserved but not a cache key
	roleValue argRole = 'v' // User-supplied data -- scrubbed to "?"
)

// commandPattern describes the argument layout after the command name.
type commandPattern struct {
	fixed     []argRole // roles for the first N positional args
	repeating []argRole // repeating group applied to remaining args (nil = no repeat)
	tailRole  argRole   // role for args that fall beyond fixed+repeating (0 defaults to roleValue)
}

// commandPatterns maps uppercase command names to their argument structure.
// Unknown commands fall back to scrubbing all arguments.
var commandPatterns = map[string]commandPattern{
	// String commands
	"GET":         {fixed: []argRole{roleKey}},
	"SET":         {fixed: []argRole{roleKey, roleValue}, tailRole: roleField},
	"SETNX":       {fixed: []argRole{roleKey, roleValue}},
	"SETEX":       {fixed: []argRole{roleKey, roleField, roleValue}},
	"PSETEX":      {fixed: []argRole{roleKey, roleField, roleValue}},
	"MGET":        {repeating: []argRole{roleKey}},
	"MSET":        {repeating: []argRole{roleKey, roleValue}},
	"MSETNX":      {repeating: []argRole{roleKey, roleValue}},
	"GETSET":      {fixed: []argRole{roleKey, roleValue}},
	"GETDEL":      {fixed: []argRole{roleKey}},
	"GETEX":       {fixed: []argRole{roleKey}, tailRole: roleField},
	"APPEND":      {fixed: []argRole{roleKey, roleValue}},
	"INCR":        {fixed: []argRole{roleKey}},
	"DECR":        {fixed: []argRole{roleKey}},
	"INCRBY":      {fixed: []argRole{roleKey, roleField}},
	"DECRBY":      {fixed: []argRole{roleKey, roleField}},
	"INCRBYFLOAT": {fixed: []argRole{roleKey, roleField}},
	"STRLEN":      {fixed: []argRole{roleKey}},
	"GETRANGE":    {fixed: []argRole{roleKey, roleField, roleField}},
	"SETRANGE":    {fixed: []argRole{roleKey, roleField, roleValue}},

	// Hash commands
	"HSET":         {fixed: []argRole{roleKey}, repeating: []argRole{roleField, roleValue}},
	"HGET":         {fixed: []argRole{roleKey, roleField}},
	"HMSET":        {fixed: []argRole{roleKey}, repeating: []argRole{roleField, roleValue}},
	"HMGET":        {fixed: []argRole{roleKey}, repeating: []argRole{roleField}},
	"HDEL":         {fixed: []argRole{roleKey}, repeating: []argRole{roleField}},
	"HEXISTS":      {fixed: []argRole{roleKey, roleField}},
	"HGETALL":      {fixed: []argRole{roleKey}},
	"HKEYS":        {fixed: []argRole{roleKey}},
	"HVALS":        {fixed: []argRole{roleKey}},
	"HLEN":         {fixed: []argRole{roleKey}},
	"HINCRBY":      {fixed: []argRole{roleKey, roleField, roleField}},
	"HINCRBYFLOAT": {fixed: []argRole{roleKey, roleField, roleField}},
	"HSETNX":       {fixed: []argRole{roleKey, roleField, roleValue}},

	// List commands
	"LPUSH":  {fixed: []argRole{roleKey}, repeating: []argRole{roleValue}},
	"RPUSH":  {fixed: []argRole{roleKey}, repeating: []argRole{roleValue}},
	"LPOP":   {fixed: []argRole{roleKey}, tailRole: roleField},
	"RPOP":   {fixed: []argRole{roleKey}, tailRole: roleField},
	"LRANGE": {fixed: []argRole{roleKey, roleField, roleField}},
	"LINDEX": {fixed: []argRole{roleKey, roleField}},
	"LSET":   {fixed: []argRole{roleKey, roleField, roleValue}},
	"LLEN":   {fixed: []argRole{roleKey}},
	"LREM":   {fixed: []argRole{roleKey, roleField, roleValue}},
	"LPOS":   {fixed: []argRole{roleKey, roleValue}, tailRole: roleField},

	// Set commands
	"SADD":        {fixed: []argRole{roleKey}, repeating: []argRole{roleValue}},
	"SREM":        {fixed: []argRole{roleKey}, repeating: []argRole{roleValue}},
	"SISMEMBER":   {fixed: []argRole{roleKey, roleValue}},
	"SMISMEMBER":  {fixed: []argRole{roleKey}, repeating: []argRole{roleValue}},
	"SMEMBERS":    {fixed: []argRole{roleKey}},
	"SCARD":       {fixed: []argRole{roleKey}},
	"SRANDMEMBER": {fixed: []argRole{roleKey}, tailRole: roleField},
	"SPOP":        {fixed: []argRole{roleKey}, tailRole: roleField},

	// Sorted set commands
	"ZADD":          {fixed: []argRole{roleKey}},
	"ZREM":          {fixed: []argRole{roleKey}, repeating: []argRole{roleValue}},
	"ZSCORE":        {fixed: []argRole{roleKey, roleValue}},
	"ZRANGE":        {fixed: []argRole{roleKey, roleField, roleField}, tailRole: roleField},
	"ZRANGEBYSCORE": {fixed: []argRole{roleKey, roleField, roleField}, tailRole: roleField},
	"ZREVRANGE":     {fixed: []argRole{roleKey, roleField, roleField}, tailRole: roleField},
	"ZRANK":         {fixed: []argRole{roleKey, roleValue}},
	"ZREVRANK":      {fixed: []argRole{roleKey, roleValue}},
	"ZCARD":         {fixed: []argRole{roleKey}},
	"ZCOUNT":        {fixed: []argRole{roleKey, roleField, roleField}},
	"ZINCRBY":       {fixed: []argRole{roleKey, roleField, roleValue}},

	// Key commands
	"DEL":       {repeating: []argRole{roleKey}},
	"EXISTS":    {repeating: []argRole{roleKey}},
	"EXPIRE":    {fixed: []argRole{roleKey, roleField}, tailRole: roleField},
	"PEXPIRE":   {fixed: []argRole{roleKey, roleField}, tailRole: roleField},
	"EXPIREAT":  {fixed: []argRole{roleKey, roleField}, tailRole: roleField},
	"PEXPIREAT": {fixed: []argRole{roleKey, roleField}, tailRole: roleField},
	"TTL":       {fixed: []argRole{roleKey}},
	"PTTL":      {fixed: []argRole{roleKey}},
	"TYPE":      {fixed: []argRole{roleKey}},
	"RENAME":    {fixed: []argRole{roleKey, roleKey}},
	"RENAMENX":  {fixed: []argRole{roleKey, roleKey}},
	"PERSIST":   {fixed: []argRole{roleKey}},
	"UNLINK":    {repeating: []argRole{roleKey}},
	"KEYS":      {fixed: []argRole{roleField}},
	"SCAN":      {fixed: []argRole{roleField}, tailRole: roleField},
	"DUMP":      {fixed: []argRole{roleKey}},
	"RESTORE":   {fixed: []argRole{roleKey, roleField, roleValue}, tailRole: roleField},

	// Pub/Sub
	"SUBSCRIBE":    {repeating: []argRole{roleKey}},
	"UNSUBSCRIBE":  {repeating: []argRole{roleKey}},
	"PSUBSCRIBE":   {repeating: []argRole{roleKey}},
	"PUNSUBSCRIBE": {repeating: []argRole{roleKey}},
	"PUBLISH":      {fixed: []argRole{roleKey, roleValue}},

	// Server / misc
	"AUTH":     {repeating: []argRole{roleValue}},
	"PING":     {},
	"ECHO":     {fixed: []argRole{roleValue}},
	"SELECT":   {fixed: []argRole{roleField}},
	"DBSIZE":   {},
	"FLUSHDB":  {tailRole: roleField},
	"FLUSHALL": {tailRole: roleField},
	"INFO":     {tailRole: roleField},
}

// deleteCommands lists commands that remove keys, mapped to cache.remove in cache mode.
var deleteCommands = map[string]bool{
	"DEL":    true,
	"UNLINK": true,
	"GETDEL": true,
}

// ScrubCommand returns a scrubbed command string with user-supplied values replaced
// by "?". Keys are always preserved. Field names (indices, flags, hash fields) are
// preserved only when sendDefaultPII is true; otherwise they are scrubbed too.
func ScrubCommand(cmds []string, sendDefaultPII bool) string {
	if len(cmds) == 0 {
		return ""
	}

	cmdName := strings.ToUpper(cmds[0])
	pattern, known := commandPatterns[cmdName]
	if !known {
		return fallbackScrub(cmds)
	}

	return applyScrubPattern(cmds, pattern, sendDefaultPII)
}

func applyScrubPattern(cmds []string, pattern commandPattern, sendDefaultPII bool) string {
	var b strings.Builder
	b.WriteString(cmds[0])

	args := cmds[1:]
	fixedLen := len(pattern.fixed)

	for i, arg := range args {
		b.WriteByte(' ')

		role := roleForPosition(i, fixedLen, pattern)
		if role == roleKey || (sendDefaultPII && role == roleField) {
			b.WriteString(arg)
		} else {
			b.WriteByte('?')
		}
	}

	return b.String()
}

func fallbackScrub(cmds []string) string {
	var b strings.Builder
	b.WriteString(cmds[0])
	for range cmds[1:] {
		b.WriteString(" ?")
	}
	return b.String()
}

// roleForPosition returns the argRole for argument at position i (0-based, after command name).
func roleForPosition(i, fixedLen int, pattern commandPattern) argRole {
	if i < fixedLen {
		return pattern.fixed[i]
	}
	if len(pattern.repeating) > 0 {
		repIdx := (i - fixedLen) % len(pattern.repeating)
		return pattern.repeating[repIdx]
	}
	if pattern.tailRole != 0 {
		return pattern.tailRole
	}
	return roleValue
}

// ExtractKeys returns the Redis keys (roleKey positions only) from a command.
// Hash field names and other metadata are excluded.
func ExtractKeys(cmds []string) []string {
	if len(cmds) < 2 {
		return nil
	}

	cmdName := strings.ToUpper(cmds[0])
	pattern, known := commandPatterns[cmdName]
	if !known {
		// Unknown commands: best-effort, return first arg as key.
		return []string{cmds[1]}
	}

	args := cmds[1:]
	fixedLen := len(pattern.fixed)
	var keys []string

	for i, arg := range args {
		if roleForPosition(i, fixedLen, pattern) == roleKey {
			keys = append(keys, arg)
		}
	}

	// For known commands, trust the pattern. If no roleKey positions exist
	// (e.g. AUTH, ECHO, SELECT), return nil to avoid leaking sensitive data.
	return keys
}

// CommandName returns the uppercase command name from a Commands() slice.
func CommandName(cmds []string) string {
	if len(cmds) > 0 {
		return strings.ToUpper(cmds[0])
	}
	return "UNKNOWN"
}

// IsDeleteCommand reports whether the command removes keys (DEL, UNLINK, GETDEL).
func IsDeleteCommand(cmds []string) bool {
	if len(cmds) == 0 {
		return false
	}
	return deleteCommands[strings.ToUpper(cmds[0])]
}
