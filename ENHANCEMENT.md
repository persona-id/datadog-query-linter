# Enhanced default_zero() Validation

## Problem Solved

Previously, the linter would pass queries wrapped in `default_zero()` even when the inner metric was invalid, because `default_zero()` masks errors by returning 0 instead of failing. This created a blind spot where invalid metrics could pass validation.

## Solution

The enhanced linter now:

1. **Detects `default_zero()` usage**: Identifies when queries use `default_zero()` wrapper functions
2. **Extracts inner queries**: Removes the `default_zero()` wrapper(s) to get the underlying metric query
3. **Validates inner queries separately**: Tests the inner query without `default_zero()` to detect masked failures
4. **Supports nested calls**: Handles multiple levels of `default_zero()` nesting
5. **Fails appropriately**: Returns non-zero exit code when `default_zero()` masks invalid metrics

## Test Cases Added

### Invalid Metric Detection
```yaml
# tests/datadogmetric-default-zero-invalid.yaml
spec:
  query: default_zero(avg:completely.invalid.metric.name{nonexistent:tag,invalid:filter})
```
**Result**: ❌ Fails linting (inner query is invalid)

### Nested default_zero() Calls
```yaml  
# tests/datadogmetric-nested-default-zero-invalid.yaml
spec:
  query: default_zero(default_zero(avg:deeply.nested.invalid.metric{fake:tag}))
```
**Result**: ❌ Fails linting (supports any level of nesting)

### Malformed Syntax Detection
```yaml
# tests/datadogmetric-default-zero-malformed.yaml  
spec:
  query: default_zero(avg:metric.name{tag:value}.invalid_function_call())
```
**Result**: ❌ Fails linting (syntax error in inner query)

## Implementation Details

### Query Analysis
- **Regular expressions**: Detect `default_zero()` function calls
- **Parentheses matching**: Properly extract inner queries from nested calls
- **Whitespace handling**: Tolerates spaces around function calls

### Validation Flow
1. Parse and validate original query (maintains backward compatibility)
2. If `default_zero()` detected, extract inner query
3. Validate inner query separately via Datadog API
4. Fail if inner query has syntax errors or metric issues
5. Warn if inner query returns no data (potential non-existent metric)

### Logging Enhancements
- **Debug logs**: Show query analysis details including nesting levels
- **Error logs**: Clear messages when `default_zero()` masks failures  
- **Warning logs**: Alert when metrics may not exist but syntax is valid

## Benefits

✅ **Prevents false positives**: No more invalid metrics passing due to `default_zero()` masking
✅ **Maintains compatibility**: Existing functionality unchanged
✅ **Comprehensive coverage**: Handles simple and complex nested scenarios
✅ **Clear feedback**: Detailed logging shows exactly what was detected and why it failed