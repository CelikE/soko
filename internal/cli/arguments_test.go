package cli

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// Inventory every application command, including executable parents. Check
// cardinality without running Git, launching editors, or touching user state.
func TestCommandArgumentInventory(t *testing.T) {
	type bounds struct{ min, max int }
	contracts := map[string]bounds{}
	for _, group := range []struct {
		commands string
		bounds
	}{
		{"init,list,pr,prune,undo,ui,health,stats,doc,version,shell-init,alias list,config path,config edit,config list,tag list,ctx list,snapshot list,worktree list,discover,discover on,discover off,discover status", bounds{0, 0}},
		{"go,open,remove,annotate,discover hook", bounds{0, 1}},
		{"cd,scan,alias remove,config get,ctx switch,ctx show,ctx drop,snapshot restore,snapshot show,snapshot drop,worktree rm", bounds{1, 1}},
		{"alias set,config set,worktree add", bounds{2, 2}},
		{"status,remotes,diff,fetch,pull,sync,report,branch,branch stale,stash,stash pop,clean,tag,worktree adopt", bounds{0, -1}},
		{"grep,apply,branch switch,tag add,tag remove,ctx save,snapshot save", bounds{1, -1}},
	} {
		for _, name := range strings.Split(group.commands, ",") {
			contracts[name] = group.bounds
		}
	}
	var walk func(*cobra.Command)
	walk = func(cmd *cobra.Command) {
		name := strings.TrimPrefix(cmd.CommandPath(), "soko ")
		if cmd.Runnable() && name != "exec" {
			if b, ok := contracts[name]; ok {
				delete(contracts, name)
				t.Run(name, func(t *testing.T) {
					if cmd.Args == nil {
						t.Fatal("runnable command has no explicit argument contract")
					}
					for n := 0; n <= 4; n++ {
						args := make([]string, n)
						for i := range args {
							args[i] = "-literal"
						}
						err := cmd.ValidateArgs(args)
						wantOK := n >= b.min && (b.max < 0 || n <= b.max)
						if (err == nil) != wantOK {
							t.Errorf("%d args: error = %v, want accepted=%t", n, err, wantOK)
						}
					}
				})
			} else {
				switch name {
				case "alias", "ctx", "snapshot", "worktree": // Help-only parents.
				default:
					t.Errorf("missing argument inventory for %s", name)
				}
			}
		}
		for _, child := range cmd.Commands() {
			walk(child)
		}
	}
	walk(NewRootCmd("test"))
	for name := range contracts {
		t.Errorf("inventory command not found: %s", name)
	}

	// exec deliberately checks its minimum in RunE for a tailored diagnostic.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	for _, args := range [][]string{{"exec"}, {"exec", "--"}} {
		out, err := runRoot(t, args...)
		if err == nil || !strings.Contains(err.Error(), "no command specified") || out != "" {
			t.Errorf("%v: output=%q error=%v", args, out, err)
		}
	}
}

func TestStatusFilterCombinations(t *testing.T) {
	results := []statusResult{
		{index: 0}, {index: 1, dirty: true}, {index: 2, ahead: true},
		{index: 3, behind: true}, {index: 4, dirty: true, ahead: true, behind: true},
		{index: 5, err: "unreadable"},
	}
	for mask := 1; mask < 16; mask++ {
		dirty, clean, ahead, behind := mask&1 != 0, mask&2 != 0, mask&4 != 0, mask&8 != 0
		got := filterResults(results, dirty, clean, ahead, behind)
		seen := map[int]bool{}
		for _, r := range got {
			if seen[r.index] {
				t.Errorf("mask %d: duplicate result %d", mask, r.index)
			}
			seen[r.index] = true
		}
		for _, r := range results {
			want := (dirty && r.dirty) || (clean && !r.dirty && !r.ahead && !r.behind && r.err == "") || (ahead && r.ahead) || (behind && r.behind)
			if seen[r.index] != want {
				t.Errorf("mask %d: result %d selected=%t, want %t", mask, r.index, seen[r.index], want)
			}
		}
	}
}
