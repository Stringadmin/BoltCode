package agent

// Tool definitions that the AI agent can use — focused on coding scenarios.
var Tools = []map[string]interface{}{
	{
		"name":        "read_file",
		"description": "Read the contents of a file.",
		"input_schema": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]string{"type": "string", "description": "File path relative to working directory"},
			},
			"required": []string{"path"},
		},
	},
	{
		"name":        "write_file",
		"description": "Create a new file or completely overwrite an existing one.",
		"input_schema": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path":    map[string]string{"type": "string", "description": "File path"},
				"content": map[string]string{"type": "string", "description": "Full file content"},
			},
			"required": []string{"path", "content"},
		},
	},
	{
		"name":        "edit_file",
		"description": "Make a targeted edit to an existing file by replacing a specific string. Use this instead of write_file when modifying existing code.",
		"input_schema": map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path":       map[string]string{"type": "string", "description": "File path"},
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
				"path": map[string]string{"type": "string", "description": "Directory (default: current dir)"},
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
	// todo: a lightweight task planner so the agent can break complex work
	// into trackable steps and not lose the thread in long sessions.
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
							"content":     map[string]string{"type": "string"},
							"status":      map[string]string{"type": "string", "description": "pending | in_progress | completed"},
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
				"path": map[string]string{"type": "string", "description": "File path"},
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
}
