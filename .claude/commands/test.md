Run the full test suite with race detection and report results.

```
make test-race
```

If any tests fail, analyze the failures and suggest fixes. Group failures by package and show the relevant error output.

After all tests pass, run coverage analysis:

```
make test 2>&1 | grep -E "^(ok|FAIL)"
```

When reviewing or writing new tests, follow the standards in `.claude/agents/test-writer.md`:
- Every test must assert a meaningful behavioral property
- Negative and boundary tests are required
- Fuzz tests required for parsing/validation code
- Don't match tests to current code — verify correctness independently
