package runner

type Language struct {
	Name       string
	Image      string
	SourceFile string
	CompileCmd []string
	RunCmd     []string
}

var Languages = map[string]Language{
	"python": {
		Name:       "python",
		Image:      "rre-python:latest",
		SourceFile: "main.py",
		RunCmd:     []string{"python3", "-B", "-u", "main.py"},
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
		CompileCmd: []string{"go", "build", "-ldflags=-s -w", "-o", "main", "main.go"},
		RunCmd:     []string{"./main"},
	},
	"javascript": {
		Name:       "javascript",
		Image:      "rre-node:latest",
		SourceFile: "main.js",
		RunCmd:     []string{"node", "main.js"},
	},
}