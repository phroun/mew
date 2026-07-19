# Accessibility Label Association Design

## Current State

Some trinkets already have built-in labels:
- Checkbox: text beside the checkbox
- RadioButton: text beside the radio button
- Button: button text

## Common Uses of Bare Labels

### Form Fields
- "Name:" / "Email:" / "Password:" before text inputs
- "Amount: $" with input after (prefix label)
- "years old" after a number input (suffix label)

### Section Headers
- "Personal Information" above a group of fields
- "Advanced Options" to introduce a collapsible section
- "Required Fields *" as a form legend

### Descriptions & Instructions
- "Enter the 6-digit code sent to your phone" above an input
- "Select one or more items:" above a ListView
- "Drag to reorder" near a sortable list

### Value Displays (Read-only)
- "Total: $149.99" (label + dynamic value)
- "Status: Connected" / "Status: Offline"
- "Last modified: Dec 30, 2024"

### List/Tree Context
- "Available Items:" above a ListView
- "Folder Contents:" above a TreeView
- "Search Results (15 found):" above results

### Error & Validation
- "This field is required" below an input
- "Invalid email format" as inline error
- "Passwords do not match"

### Help & Hints
- "Tip: Use Tab to navigate" at bottom of dialog
- "Press F1 for help"
- "(optional)" after a field label

### Dialog Content
- "Are you sure you want to delete 'document.txt'?"
- "This action cannot be undone."

## Association Patterns

| Pattern | Example | Association |
|---------|---------|-------------|
| **Preceding** | `[Name:] [____]` | Label -> next focusable |
| **Above** | `Items:` newline `[ListView]` | Label -> trinket below |
| **Suffix** | `[___] years old` | Label -> preceding trinket |
| **Group header** | `Address:` newline `Street, City, Zip` | Label -> all in group |
| **Descriptive** | `[input]` newline `Must be 8+ chars` | Trinket -> description below |

## Label Types to Distinguish

- **Primary label** ("Name:") - announced when focusing
- **Description** ("Must be 8+ characters") - additional context
- **Section header** ("Personal Info") - context for multiple fields
- **Standalone** ("Welcome!") - not associated with anything

## Implementation Options

1. **Explicit association**: `label.SetLabelFor(trinket)` or `trinket.SetAccessibleLabel(label)`
2. **Layout-based**: BoxLayout knows preceding Label should label next focusable trinket
3. **Name-based**: Trinket's accessible name is set directly without Label trinket
4. **Proximity-based**: Automatic detection based on layout position

## Decision Needed

Which approach (or combination) should we implement?
