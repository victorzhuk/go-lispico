# Code Style Conventions

## Naming
- Variables: `cfg`, `repo`, `srv`, `ctx`, `err` (natural, concise)
- Structs: Private by default (`type engine struct`)
- Public only for domain: `type Value interface`
- Receivers: Short (`e *engine`, `v Value`)

## Error Handling
- Format: lowercase, no trailing punctuation
- Always wrap: `fmt.Errorf("context: %w", err)`
- No bare `return err`

## Imports
- Group: stdlib, blank line, internal, blank line, external

## Comments
- ZERO comments explaining WHAT (rename instead)
- Comments for WHY only

## Testing
- Table-driven tests with `t.Run`
- Use `t.Parallel()` where possible
