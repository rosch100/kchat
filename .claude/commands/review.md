Review the current uncommitted changes using the reviewer agent.

1. Run `git diff` to see staged and unstaged changes
2. Run `git diff --cached` to see only staged changes
3. Analyze changes with special attention to:
   - Echo prevention layer integrity
   - Puppet resolution order
   - Missing tests for new functionality
   - Race conditions in concurrent code
   - Proper zerolog usage
4. Provide a structured review with summary, risk assessment, issues, and suggestions
