// Package misetrust has mise trust the configs in a directory smith owns, one
// invocation at a time. smith runs mise against worktrees it cut and configs
// it generated, and a mise that refuses an untrusted config there (as it does
// in paranoid mode) would fail, or run without the repo's tools and env.
//
// Trust travels in the environment of the one invocation, appended to what
// that environment already trusts, so it never widens past the directory named,
// never replaces trust the operator granted, and leaves nothing in mise's trust
// store or the operator's config to clean up afterwards.
package misetrust

// Var is the environment variable mise reads extra trusted config paths from,
// colon separated. A path trusts every config at or below it.
const Var = "MISE_TRUSTED_CONFIG_PATHS"

// Script is the shell script Command runs mise through: it appends the
// directory $0 names to the trusted config paths of the environment it runs
// in, then runs its arguments as the command.
const Script = `export ` + Var + `="${` + Var + `:+$` + Var + `:}$0" && exec "$@"`

// Command is the argv that runs mise with args, trusting the configs at or
// below dir on top of whatever the environment it runs in already trusts.
func Command(mise, dir string, args ...string) []string {
	return append([]string{"sh", "-c", Script, dir, mise}, args...)
}
