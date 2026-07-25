# Debug Session: metadata-oom
- **Status**: [OPEN]
- **Issue**: TUI running metadata sync still crashes with Node.js heap out of memory after long execution.
- **Debug Server**: http://127.0.0.1:7777/event
- **Log File**: .dbg/trae-debug-log-metadata-oom.ndjson

## Reproduction Steps
1. Run the TUI with `npm run tui`.
2. Start a metadata sync task against the PyPI source.
3. Wait until memory usage grows and Node exits with heap OOM.

## Hypotheses & Verification
| ID | Hypothesis | Likelihood | Effort | Evidence |
|----|------------|------------|--------|----------|
| A | Metadata fetch flow still retains large in-memory structures during the run | High | Medium | Pending |
| B | TUI state updates accumulate too much log/progress data and drive heap growth | High | Low | Pending |
| C | Progress reporting copies large active arrays too often and amplifies memory usage | Medium | Low | Pending |
| D | An upstream phase still materializes all work items before workers consume them | Medium | Medium | Pending |
| E | Manifest build holds all artifacts in memory and grows superlinearly while counting per package | High | Low | Pending |

## Log Evidence
Pending.

## Verification Conclusion
Pending.
