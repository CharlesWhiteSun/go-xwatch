package cli

// Command defines a runnable CLI subcommand.
type Command interface {
	Name() string
	Run(args []string) error
}

// CommandFunc adapts a function to Command.
type CommandFunc struct {
	CommandName string
	Fn          func(args []string) error
}

func (c CommandFunc) Name() string            { return c.CommandName }
func (c CommandFunc) Run(args []string) error { return c.Fn(args) }

// Registry stores command mappings.
type Registry struct {
	m map[string]Command
}

// NewRegistry initializes a registry with optional commands.
func NewRegistry(cmds ...Command) *Registry {
	r := &Registry{m: make(map[string]Command)}
	for _, c := range cmds {
		r.Register(c)
	}
	return r
}

// Register adds or replaces a command by name.
func (r *Registry) Register(c Command) {
	if r.m == nil {
		r.m = make(map[string]Command)
	}
	r.m[c.Name()] = c
}

// Get returns a command by name (empty string safe).
func (r *Registry) Get(name string) (Command, bool) {
	c, ok := r.m[name]
	return c, ok
}

// Names returns all registered command names.
func (r *Registry) Names() []string {
	out := make([]string, 0, len(r.m))
	for k := range r.m {
		out = append(out, k)
	}
	return out
}
