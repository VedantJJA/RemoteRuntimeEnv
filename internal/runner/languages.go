package runner

// Language describes how a submission in a given language is compiled and run
// inside its sandbox container. Each language gets its own minimal image
// (see /images) so containers start fast and carry no unnecessary tooling.
type Language struct {
	Name       string
	Image      string
	SourceFile string
	// CompileCmd is run once before execution. Empty means no compile step
	// (interpreted languages). Failures here are reported as CompileError,
	// distinct from RuntimeError, so the frontend can show the right message.
	CompileCmd []string
	// RunCmd executes the compiled/interpreted program. Stdin is piped in;
	// stdout/stderr are captured separately.
	RunCmd []string
}

var Languages = map[string]Language{
	"python": {
		Name:       "python",
		Image:      "rre-python:latest",
		SourceFile: "main.py",
		RunCmd:     []string{"python3", "main.py"},
	},
	"cpp": {
		Name:       "cpp",
		Image:      "rre-cpp:latest",
		SourceFile: "main.cpp",
		CompileCmd: []string{"g++", "-O2", "-std=c++17", "-o", "main", "main.cpp"},
		RunCmd:     []string{"./main"},
	},
	"go": {
		Name:       "go",
		Image:      "rre-go:latest",
		SourceFile: "main.go",
		CompileCmd: []string{"go", "build", "-o", "main", "main.go"},
		RunCmd:     []string{"./main"},
	},
	"javascript": {
		Name:       "javascript",
		Image:      "rre-node:latest",
		SourceFile: "main.js",
		RunCmd:     []string{"node", "main.js"},
	},
}
