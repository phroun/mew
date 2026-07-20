// Package keys provides key sequence processing for the editor.
package keys

import (
	"sort"
	"strings"
	"unicode"
)

// ProcessResult represents the result of processing a key.
type ProcessResult struct {
	Command string // Command to execute (empty if none)
	Handled bool   // Whether the key was handled
}

// CommandExecutor is a callback for executing commands.
type CommandExecutor func(command string)

// SequenceProcessor handles key sequence detection and processing.
// It supports multi-key sequences like ^K X (Ctrl-K followed by X),
// with disambiguation for sequences that could be prefixes of longer sequences.
type SequenceProcessor struct {
	keyMap   map[string]string // Key sequence -> command mapping
	executor CommandExecutor   // Command executor callback

	// Key alias mappings for fallbacks
	keyAliases     map[string]string
	simpleControls map[string]string

	// macOptionInsert re-inserts macOS Option characters for unmapped Meta
	// keys (see getDefaultHandling / SetMacOptionInsert).
	macOptionInsert bool

	// Sequence tracking
	sequenceStarters        map[string]bool
	controlSequenceStarters map[string]bool

	// Current state
	activeSequence       string
	pendingShortMatch    string
	pendingFallbackMatch string
	keyBuffer            []string
	isReprocessing       bool

	debug bool
}

// NewSequenceProcessor creates a new key sequence processor.
func NewSequenceProcessor(executor CommandExecutor) *SequenceProcessor {
	sp := &SequenceProcessor{
		keyMap:                  make(map[string]string),
		executor:                executor,
		keyAliases:              make(map[string]string),
		simpleControls:          make(map[string]string),
		sequenceStarters:        make(map[string]bool),
		controlSequenceStarters: make(map[string]bool),
		keyBuffer:               make([]string, 0),
	}
	sp.initializeKeyAliases()
	return sp
}

// initializeKeyAliases sets up key alias mappings.
func (sp *SequenceProcessor) initializeKeyAliases() {
	// Equivalent keys (first entry is primary, others are aliases)
	aliasGroups := [][]string{
		{"back", "^H", "backspace"},
		{"tab", "^I"},
		{"return", "enter", "^M"},
		{"fdel", "delete"},
		{"^space", "^2"},
		{"esc", "escape", "^[", "^3"},
		{"^\\", "^4"},
		{"^]", "^5"},
		{"^^", "^6"},
		{"^_", "^7"},
		{"del", "^8"},
	}

	for _, group := range aliasGroups {
		primary := group[0]
		for _, alias := range group[1:] {
			if len(alias) == 2 && alias[0] == '^' {
				sp.simpleControls[primary] = alias
			}
			sp.keyAliases[alias] = primary
		}
	}
}

// MapKey maps a key sequence to a command.
func (sp *SequenceProcessor) MapKey(keySequence, command string) {
	sp.keyMap[keySequence] = command
	sp.updateSequenceStarters()
}

// UnmapKey removes a key mapping.
func (sp *SequenceProcessor) UnmapKey(keySequence string) {
	delete(sp.keyMap, keySequence)
	sp.updateSequenceStarters()
}

// GetMapping returns the command mapped to a key sequence, or empty string if not found.
func (sp *SequenceProcessor) GetMapping(keySequence string) string {
	return sp.keyMap[keySequence]
}

// SetMappings replaces the entire keymap with a copy of m (used to switch the
// active mapping set when the focused window changes).
func (sp *SequenceProcessor) SetMappings(m map[string]string) {
	sp.keyMap = make(map[string]string, len(m))
	for k, v := range m {
		sp.keyMap[k] = v
	}
	sp.updateSequenceStarters()
}

// GetAllMappings returns a copy of all key mappings.
func (sp *SequenceProcessor) GetAllMappings() map[string]string {
	result := make(map[string]string, len(sp.keyMap))
	for k, v := range sp.keyMap {
		result[k] = v
	}
	return result
}

// updateSequenceStarters rebuilds the set of sequence starters from the key map.
func (sp *SequenceProcessor) updateSequenceStarters() {
	sp.sequenceStarters = make(map[string]bool)
	sp.controlSequenceStarters = make(map[string]bool)

	for key := range sp.keyMap {
		if strings.Contains(key, " ") {
			parts := strings.Split(key, " ")
			firstPart := parts[0]
			sp.sequenceStarters[firstPart] = true

			if strings.HasPrefix(firstPart, "^") {
				sp.controlSequenceStarters[firstPart] = true
			}
		}
	}
}

// GetActiveSequence returns the current active sequence.
func (sp *SequenceProcessor) GetActiveSequence() string {
	return sp.activeSequence
}

// ClearActiveSequence clears the current sequence state.
func (sp *SequenceProcessor) ClearActiveSequence() {
	sp.activeSequence = ""
	sp.pendingShortMatch = ""
	sp.pendingFallbackMatch = ""
}

// ProcessKey processes a key input and returns the result.
func (sp *SequenceProcessor) ProcessKey(key string) ProcessResult {
	// Don't process empty keys (likely timeout signals)
	if key == "" {
		return ProcessResult{Command: "", Handled: true}
	}

	// If we have an active sequence, handle accordingly
	if sp.activeSequence != "" {
		return sp.handleKeyWithActiveSequence(key)
	}

	// Check if this is a sequence starter
	if sp.isSequenceStarter(key) {
		sp.activeSequence = key
		return ProcessResult{Command: "", Handled: true}
	}

	// Direct mapping without a sequence
	if result := sp.getCommand(key); result != nil {
		return ProcessResult{Command: result.command, Handled: true}
	}

	// Default handling for unmapped keys
	command := sp.getDefaultHandling(key)
	return ProcessResult{Command: command, Handled: true}
}

// commandMatch holds a matched command and metadata.
type commandMatch struct {
	command         string
	matchedSequence string
	isFallback      bool
}

// getCommand looks up a command for a key sequence, including fallbacks.
func (sp *SequenceProcessor) getCommand(keySequence string) *commandMatch {
	// Try direct match first
	if cmd, ok := sp.keyMap[keySequence]; ok {
		return &commandMatch{
			command:         cmd,
			matchedSequence: keySequence,
			isFallback:      false,
		}
	}

	// Try fallbacks
	for _, fallback := range sp.getKeyFallbacks(keySequence) {
		if fallback == keySequence {
			continue
		}
		if cmd, ok := sp.keyMap[fallback]; ok {
			return &commandMatch{
				command:         cmd,
				matchedSequence: fallback,
				isFallback:      true,
			}
		}
	}

	return nil
}

// getKeyFallbacks generates alternative sequences to try.
func (sp *SequenceProcessor) getKeyFallbacks(sequence string) []string {
	fallbacks := []string{sequence}

	// Single character case variants
	if len(sequence) == 1 {
		r := rune(sequence[0])
		if unicode.IsLetter(r) {
			if unicode.IsUpper(r) {
				fallbacks = append(fallbacks, strings.ToLower(sequence))
			} else {
				fallbacks = append(fallbacks, strings.ToUpper(sequence))
			}
		}
	}

	// Multi-part sequence handling
	if strings.Contains(sequence, " ") {
		parts := strings.Split(sequence, " ")
		firstPart := parts[0]
		isControlStarter := sp.controlSequenceStarters[firstPart]

		for i := 1; i < len(parts); i++ {
			part := parts[i]

			// Case insensitivity for single letters
			if len(part) == 1 {
				r := rune(part[0])
				if unicode.IsLetter(r) {
					altParts := make([]string, len(parts))
					copy(altParts, parts)
					if unicode.IsUpper(r) {
						altParts[i] = strings.ToLower(part)
					} else {
						altParts[i] = strings.ToUpper(part)
					}
					fallbacks = append(fallbacks, strings.Join(altParts, " "))
				}
			}

			// Control character fallbacks for control sequence starters
			if isControlStarter {
				if controlVersion := sp.getSimpleControl(part); controlVersion != "" {
					// Try control version
					altParts := make([]string, len(parts))
					copy(altParts, parts)
					altParts[i] = controlVersion
					fallbacks = append(fallbacks, strings.Join(altParts, " "))

					// Try non-control version
					if len(controlVersion) == 2 && controlVersion[0] == '^' {
						nonControlVersion := string(controlVersion[1])
						altParts2 := make([]string, len(parts))
						copy(altParts2, parts)
						altParts2[i] = nonControlVersion
						fallbacks = append(fallbacks, strings.Join(altParts2, " "))
					}
				}
			}
		}
	}

	// Check for key aliases
	for alias, primary := range sp.keyAliases {
		if strings.HasSuffix(sequence, primary) {
			fallbacks = append(fallbacks, sequence[:len(sequence)-len(primary)]+alias)
		}
		if strings.HasSuffix(sequence, alias) {
			fallbacks = append(fallbacks, sequence[:len(sequence)-len(alias)]+primary)
		}
	}

	return fallbacks
}

// getSimpleControl converts a key to its control character equivalent.
func (sp *SequenceProcessor) getSimpleControl(key string) string {
	if ctrl, ok := sp.simpleControls[key]; ok {
		return ctrl
	}
	if len(key) == 2 && key[0] == '^' {
		return key
	}
	return ""
}

// isSequenceStarter checks if a key starts a multi-key sequence.
func (sp *SequenceProcessor) isSequenceStarter(key string) bool {
	if sp.sequenceStarters[key] {
		return true
	}

	// Check if any mapped sequence starts with this key plus a space
	searchPattern := key + " "
	for mappedKey := range sp.keyMap {
		if strings.HasPrefix(mappedKey, searchPattern) {
			sp.sequenceStarters[key] = true
			return true
		}
	}

	return false
}

// isPotentialSequenceMatch checks if a sequence could match a longer sequence.
func (sp *SequenceProcessor) isPotentialSequenceMatch(sequence string) bool {
	// Check direct prefix matches
	for mappedKey := range sp.keyMap {
		if strings.HasPrefix(mappedKey, sequence) {
			return true
		}
	}

	// Check fallback prefixes
	for _, fallback := range sp.getKeyFallbacks(sequence) {
		if fallback == sequence {
			continue
		}
		for mappedKey := range sp.keyMap {
			if strings.HasPrefix(mappedKey, fallback) {
				return true
			}
		}
	}

	return false
}

// hasLongerPotentialMatches checks if there are longer matches possible.
func (sp *SequenceProcessor) hasLongerPotentialMatches(sequence, matchedSequence string) bool {
	searchPrefix := sequence + " "
	for mappedKey := range sp.keyMap {
		if strings.HasPrefix(mappedKey, searchPrefix) {
			return true
		}
	}

	if matchedSequence != "" && matchedSequence != sequence {
		fallbackPrefix := matchedSequence + " "
		for mappedKey := range sp.keyMap {
			if strings.HasPrefix(mappedKey, fallbackPrefix) {
				return true
			}
		}
	}

	for _, fallback := range sp.getKeyFallbacks(sequence) {
		if fallback == sequence {
			continue
		}
		fallbackPrefix := fallback + " "
		for mappedKey := range sp.keyMap {
			if strings.HasPrefix(mappedKey, fallbackPrefix) {
				return true
			}
		}
	}

	return false
}

// handleKeyWithActiveSequence processes a key when there's an active sequence.
func (sp *SequenceProcessor) handleKeyWithActiveSequence(key string) ProcessResult {
	// Handle pending match disambiguation
	if sp.pendingShortMatch != "" {
		sp.keyBuffer = append(sp.keyBuffer, key)

		fullSequence := sp.activeSequence + " " + key
		isPotentialMatch := sp.isPotentialSequenceMatch(fullSequence)

		if !isPotentialMatch && sp.pendingFallbackMatch != "" {
			fallbackFullSeq := sp.pendingFallbackMatch + " " + key
			isPotentialMatch = sp.isPotentialSequenceMatch(fallbackFullSeq)
		}

		if !isPotentialMatch {
			sp.processPendingMatch()
			return ProcessResult{Command: "", Handled: true}
		}
	}

	fullSequence := sp.activeSequence + " " + key

	// Check for command match
	if result := sp.getCommand(fullSequence); result != nil {
		if sp.hasLongerPotentialMatches(fullSequence, result.matchedSequence) {
			// Store as pending and wait for more input
			sp.pendingShortMatch = fullSequence
			if result.isFallback {
				sp.pendingFallbackMatch = result.matchedSequence
			} else {
				sp.pendingFallbackMatch = ""
			}
			sp.keyBuffer = nil
			sp.activeSequence = fullSequence
			return ProcessResult{Command: "", Handled: true}
		}

		// No longer matches, execute immediately
		sp.ClearActiveSequence()
		return ProcessResult{Command: result.command, Handled: true}
	}

	// Check if this could be a prefix
	if sp.isPotentialSequenceMatch(fullSequence) {
		sp.activeSequence = fullSequence
		return ProcessResult{Command: "", Handled: true}
	}

	// Handle pending short match if sequence invalid
	if sp.pendingShortMatch != "" {
		sp.processPendingMatch()
		return ProcessResult{Command: "", Handled: true}
	}

	// Sequence not recognized - handle fallback
	parts := strings.Split(sp.activeSequence, " ")
	sequenceStarter := parts[0]
	sp.ClearActiveSequence()

	// For single-char starters, insert them as text
	if len(sequenceStarter) == 1 {
		starterCommand := "insert '" + escapeStringLiteral(sequenceStarter) + "'"
		if sp.executor != nil {
			sp.executor(starterCommand)
		}

		// Reprocess remaining parts
		for i := 1; i < len(parts); i++ {
			sp.handleSingleKey(parts[i])
		}

		sp.handleSingleKey(key)
		return ProcessResult{Command: "", Handled: true}
	}

	// For control starters, just reprocess remaining parts
	sp.handleSingleKey(key)
	return ProcessResult{Command: "", Handled: true}
}

// processPendingMatch executes a pending match and reprocesses buffered keys.
func (sp *SequenceProcessor) processPendingMatch() {
	if sp.pendingShortMatch == "" {
		return
	}

	result := sp.getCommand(sp.pendingShortMatch)
	keysToReprocess := sp.keyBuffer

	sp.pendingShortMatch = ""
	sp.pendingFallbackMatch = ""
	sp.keyBuffer = nil
	sp.ClearActiveSequence()

	if result != nil && sp.executor != nil {
		sp.executor(result.command)

		sp.isReprocessing = true
		for _, k := range keysToReprocess {
			sp.handleSingleKey(k)
		}
		sp.isReprocessing = false
	}
}

// handleSingleKey processes a single key outside of sequence tracking.
func (sp *SequenceProcessor) handleSingleKey(key string) {
	// Check if sequence starter
	if sp.isSequenceStarter(key) {
		sp.activeSequence = key
		return
	}

	// Check for direct command
	if result := sp.getCommand(key); result != nil {
		if sp.executor != nil {
			sp.executor(result.command)
		}
		return
	}

	// Default handling
	command := sp.getDefaultHandling(key)
	if command != "" && sp.executor != nil {
		sp.executor(command)
	}
}

// getDefaultHandling returns the default command for unmapped keys.
func (sp *SequenceProcessor) getDefaultHandling(key string) string {
	switch key {
	case "space":
		return "insert ' '"
	case "del", "back":
		return "del_char_prior"
	case "return":
		return "nav_follow|accept|insert '\\n'"
	case "^C":
		return "nav_cancel|cancel|buffer_close"
	case "esc":
		return "cmd"
	default:
		// Insert a single typed character. A longer key is an unmapped named
		// or modified key (e.g. "F1", "ins", "pgup").
		if len([]rune(key)) == 1 {
			return "insert '" + escapeStringLiteral(key) + "'"
		}
		// An unmapped Meta combination reverse-maps to the character macOS
		// Option would have typed, so bindings steal individual Option
		// combos while the rest insert seamlessly (and Alt on any platform
		// gains the same mac-style character layer).
		if sp.macOptionInsert {
			if ch, ok := macOptionChars[key]; ok {
				return "insert '" + escapeStringLiteral(ch) + "'"
			}
		}
		return ""
	}
}

// SetMacOptionInsert enables re-inserting the macOS Option character for
// unmapped Meta keys (see getDefaultHandling).
func (sp *SequenceProcessor) SetMacOptionInsert(enabled bool) {
	sp.macOptionInsert = enabled
}

// GetPossibleCompletions returns possible completions for the current sequence.
// Returns completions sorted alphabetically to match TypeScript behavior.
func (sp *SequenceProcessor) GetPossibleCompletions() []string {
	if sp.activeSequence == "" {
		return nil
	}

	completions := make(map[string]bool)
	prefix := sp.activeSequence + " "

	for mappedKey := range sp.keyMap {
		if strings.HasPrefix(mappedKey, prefix) {
			nextPart := strings.TrimPrefix(mappedKey, prefix)
			nextKey := strings.Split(nextPart, " ")[0]
			completions[nextKey] = true
		}
	}

	// Check fallbacks too
	for _, fallback := range sp.getKeyFallbacks(sp.activeSequence) {
		if fallback == sp.activeSequence {
			continue
		}
		fallbackPrefix := fallback + " "
		for mappedKey := range sp.keyMap {
			if strings.HasPrefix(mappedKey, fallbackPrefix) {
				nextPart := strings.TrimPrefix(mappedKey, fallbackPrefix)
				nextKey := strings.Split(nextPart, " ")[0]
				completions[nextKey] = true
			}
		}
	}

	result := make([]string, 0, len(completions))
	for k := range completions {
		result = append(result, k)
	}

	// Sort for deterministic output (matches TypeScript behavior)
	sort.Strings(result)

	return result
}

// DumpKeyMap returns a debug representation of the key map.
func (sp *SequenceProcessor) DumpKeyMap() string {
	var sb strings.Builder
	sb.WriteString("Key Map:\n")
	for k, v := range sp.keyMap {
		sb.WriteString("  ")
		sb.WriteString(k)
		sb.WriteString(" -> ")
		sb.WriteString(v)
		sb.WriteString("\n")
	}
	return sb.String()
}

// escapeStringLiteral escapes a string for use in a command string literal.
func escapeStringLiteral(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "'", "\\'")
	s = strings.ReplaceAll(s, "\n", "\\n")
	s = strings.ReplaceAll(s, "\r", "\\r")
	s = strings.ReplaceAll(s, "\t", "\\t")
	return s
}
