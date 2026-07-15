package agent

// Tool definitions that the AI agent can use — focused on coding scenarios.
var Tools = []map[string]interface{}{
	{
		"name":        "read_file",
		"description": "Read the contents of a file. Supports optional offset (1-based line number to start from) and limit (number of lines to read). Use these for large files to avoid filling the context window.",
		"input_schema": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path":   map[string]string{"type": "string", "description": "File path relative to working directory"},
				"offset": map[string]string{"type": "string", "description": "Line number to start reading from (1-based). If omitted, starts from line 1."},
				"limit":  map[string]string{"type": "string", "description": "Maximum number of lines to read. If omitted, reads the entire file."},
			},
			"required": []string{"path"},
		},
	},
	{
		"name":        "write_file",
		"description": "Create a new file or completely overwrite an existing one. Prefer a path relative to the working directory.",
		"input_schema": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path":    map[string]string{"type": "string", "description": "File path relative to working directory"},
				"content": map[string]string{"type": "string", "description": "Full file content"},
			},
			"required": []string{"path", "content"},
		},
	},
	{
		"name":        "edit_file",
		"description": "Make a targeted edit to an existing file by replacing a specific string. Use this instead of write_file when modifying existing code. Prefer a path relative to the working directory.",
		"input_schema": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path":       map[string]string{"type": "string", "description": "File path relative to working directory"},
				"old_string": map[string]string{"type": "string", "description": "Exact string to find and replace"},
				"new_string": map[string]string{"type": "string", "description": "Replacement string"},
			},
			"required": []string{"path", "old_string", "new_string"},
		},
	},
	{
		"name":        "search_code",
		"description": "Search for a pattern across files using grep. Returns matching lines with file paths and line numbers.",
		"input_schema": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"pattern": map[string]string{"type": "string", "description": "Search pattern (regex supported)"},
				"path":    map[string]string{"type": "string", "description": "Directory or file to search in (default: .)"},
				"glob":    map[string]string{"type": "string", "description": "File glob filter, e.g. *.go, *.ts"},
			},
			"required": []string{"pattern"},
		},
	},
	{
		"name":        "run_command",
		"description": "Execute a shell command. Use for building, testing, installing deps, git operations.",
		"input_schema": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"command": map[string]string{"type": "string", "description": "Shell command to execute"},
			},
			"required": []string{"command"},
		},
	},
	{
		"name":        "list_files",
		"description": "List files in a directory recursively (excludes .git, node_modules, etc).",
		"input_schema": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]string{"type": "string", "description": "Directory relative to working directory (default: current dir)"},
			},
		},
	},
	{
		"name":        "update_memory",
		"description": "Persist project knowledge across sessions. Use this to remember: project language, framework, build/test commands, what each file does, user preferences, and architectural decisions. This memory will be available in future sessions.",
		"input_schema": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"key":   map[string]string{"type": "string", "description": "One of: language, framework, build_cmd, test_cmd, architecture, file:<path>, preference, decision"},
				"value": map[string]string{"type": "string", "description": "The information to remember. For 'decision', use the format 'topic: choice'"},
			},
			"required": []string{"key", "value"},
		},
	},
	{
		"name":        "todo_write",
		"description": "Create or replace the task list for the current session. Use this to plan multi-step work up front, then mark items in_progress/completed as you go. An empty list clears it.",
		"input_schema": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"todos": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"content": map[string]string{"type": "string"},
							"status":  map[string]string{"type": "string", "description": "pending | in_progress | completed"},
						},
						"required": []string{"content", "status"},
					},
				},
			},
			"required": []string{"todos"},
		},
	},
	{
		"name":        "multi_edit",
		"description": "Apply several precise edits to one file in one call. Each edit replaces old_string with new_string; old_string must be unique in the file. Use this instead of repeated edit_file when modifying the same file multiple times.",
		"input_schema": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]string{"type": "string", "description": "File path relative to working directory"},
				"edits": map[string]interface{}{
					"type": "array",
					"items": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"old_string": map[string]string{"type": "string"},
							"new_string": map[string]string{"type": "string"},
						},
						"required": []string{"old_string", "new_string"},
					},
				},
			},
			"required": []string{"path", "edits"},
		},
	},
	{
		"name":        "delete_file",
		"description": "Delete a file or an empty directory. Use when removing dead code, deprecated files, or empty directories. Will NOT delete non-empty directories.",
		"input_schema": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]string{"type": "string", "description": "Path to the file or empty directory to delete"},
			},
			"required": []string{"path"},
		},
	},
	{
		"name":        "rename_file",
		"description": "Rename or move a file or directory. Use for refactoring: renaming a file to a new name, moving it to a new directory, or both. Creates parent directories of the destination if needed.",
		"input_schema": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"from": map[string]string{"type": "string", "description": "Current path"},
				"to":   map[string]string{"type": "string", "description": "New path"},
			},
			"required": []string{"from", "to"},
		},
	},
	{
		"name":        "web_fetch",
		"description": "Fetch a URL and return its text content. Use to read documentation, API specs, package READMEs, or any web resource needed during development. Returns the raw text — use for reference, not user-facing display.",
		"input_schema": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"url": map[string]string{"type": "string", "description": "Full URL to fetch (https://...)"},
			},
			"required": []string{"url"},
		},
	},
	{
		"name":        "git_status",
		"description": "Show the working tree status — modified, staged, and untracked files. Use before committing to review what changed, or to check if the working tree is clean.",
		"input_schema": map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
	},
	{
		"name":        "git_diff",
		"description": "Show the diff of uncommitted changes. Use to review what you've changed before committing, or to inspect the exact modifications made so far.",
		"input_schema": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]string{"type": "string", "description": "Optional: show diff only for this file path"},
			},
		},
	},
	{
		"name":        "git_commit",
		"description": "Stage all changes and create a commit with a given message. Use after completing a logical unit of work so progress is saved in git history. The commit is created in the agent's working directory.",
		"input_schema": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"message": map[string]string{"type": "string", "description": "Commit message describing what was done"},
			},
			"required": []string{"message"},
		},
	},
	{
		"name":        "get_diagnostics",
		"description": "Run a diagnostic/lint command (e.g. 'go vet', 'tsc --noEmit', 'cargo check') and return the output. Use after making changes to verify code correctness before committing. The command is loaded from project memory (test_cmd / build_cmd), or falls back to detecting the project type.",
		"input_schema": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"command": map[string]string{"type": "string", "description": "Optional: specific command to run. Defaults to the project's configured diagnostic command."},
			},
		},
	},
	{
		"name":        "glob",
		"description": "Find files matching a glob pattern. Use to locate files by name pattern rather than searching file contents. Supports ** for recursive matching (e.g. '**/*.go', 'src/**/*.ts', '**/*test*').",
		"input_schema": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"pattern": map[string]string{"type": "string", "description": "Glob pattern (e.g. '**/*.go', 'src/**/*.ts', '*.md')"},
				"path":    map[string]string{"type": "string", "description": "Directory to search in (default: current dir)"},
			},
			"required": []string{"pattern"},
		},
	},
}
