// Package keys provides key sequence processing for the editor.
package keys

import (
	"sort"
	"strings"
	"unicode"
)

// virtualHelpKey is the reserved pseudo-key whose mapping names the Quick Help
// topic for a prefix (see HelpTopic). It is not a real key and is filtered out
// of key completions.
const virtualHelpKey = "help"

// wildcardKey is the mapping key that matches ANY single key event at its
// level. It never participates in sequences and never appears in completions:
// it is a level's answer for the keys the level does not name.
const wildcardKey = "*"

// ProcessResult represents the result of processing a key.
type ProcessResult struct {
	Command string // Command resolved (resolution-only mode; empty when the processor executed)
	Handled bool   // Whether the key was handled
}

// CommandExecutor executes one command on behalf of the processor and reports
// whether it HANDLED the key. Only a clean false status means "not mine": that
// is a binding's way of declining the key, dropping resolution to the next
// level down. Anything else — success, an async suspension, an error — holds
// the key, so a command that merely went wrong can never volunteer its
// keystroke to a layer below it.
//
// key is the key event being resolved (the final key, for a sequence match).
//
// A nil executor puts the processor in resolution-only mode: ProcessKey
// reports the top-precedence command in ProcessResult.Command instead of
// executing, and never descends, because there is no status to descend on.
type CommandExecutor func(key, command string) bool

// levelBindings is one precedence level's slice of the keymap: its named
// sequences, and its wildcard if it declared one.
type levelBindings struct {
	specific map[string]string // sequence -> command
	wildcard string
	hasWild  bool
}

// candidate is one level's claim on a key event.
type candidate struct {
	level           int
	command         string
	wildcard        bool
	matchedSequence string // the spelling that matched (aliases; the pending machinery reads it)
	isFallback      bool
}

// SequenceProcessor handles key sequence detection and processing.
// It supports multi-key sequences like ^K X (Ctrl-K followed by X),
// with disambiguation for sequences that could be prefixes of longer
// sequences.
//
// Bindings carry a precedence LEVEL, written as leading "capture" (+1) /
// "override" (+2) words on the mapping key — `capture * = tinput_key` — and
// resolution runs from the highest level down:
//
//  1. The longest live sequence possibility wins, across all levels: a key
//     that could begin (or extend) a mapped sequence is held, wildcards
//     notwithstanding. Nothing single-key outranks a chord in progress.
//  2. Among matches of equal length, the highest level is considered first.
//  3. Within a level, a specific binding SHADOWS the wildcard — each level
//     contributes at most one candidate, so `capture ^C = false` really does
//     drop ^C past the whole capture level, not into its own wildcard.
//  4. A candidate declining (clean false status) drops consideration one
//     level and re-runs. Exhausted candidates fall to the default handling
//     floor — what an unbound key would have done all along.
//  5. A sequence disqualified by an invalid continuation unwinds: the starter
//     is RELEASED (offered to the wildcards, then its literal self), and the
//     rest replay as ordinary keys.
type SequenceProcessor struct {
	// rawMap holds bindings exactly as configured, level prefixes included.
	// It is the API surface (MapKey/GetMapping/GetAllMappings) so config
	// merging and provenance see the same spellings the user wrote; the
	// parsed view below is derived from it.
	rawMap   map[string]string
	executor CommandExecutor

	// Parsed view: per-level bindings, levels in descending order, and the
	// union of every level's specific sequences (starter/prefix/completion
	// checks care only that a sequence exists somewhere).
	levels     map[int]*levelBindings
	levelOrder []int
	allKeys    map[string]bool

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
		rawMap:                  make(map[string]string),
		executor:                executor,
		keyAliases:              make(map[string]string),
		simpleControls:          make(map[string]string),
		sequenceStarters:        make(map[string]bool),
		controlSequenceStarters: make(map[string]bool),
		keyBuffer:               make([]string, 0),
	}
	sp.initializeKeyAliases()
	sp.rebuild()
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

// parseRawKey splits a raw mapping key into its precedence level and the
// physical key sequence. Each leading "capture" word adds 1 and each
// "override" adds 2; they compound, so "capture capture ^C" sits at level 2
// and a user can always outbid a layer by writing one more word. The
// remainder is the sequence; a remainder of exactly "*" is that level's
// single-key wildcard.
func parseRawKey(raw string) (level int, seq string) {
	tokens := strings.Fields(raw)
	for i := 0; i < len(tokens); i++ {
		switch tokens[i] {
		case "capture":
			level++
			continue
		case "override":
			level += 2
			continue
		}
		return level, strings.Join(tokens[i:], " ")
	}
	// Nothing but prefix words: someone bound the literal word. Treat the
	// whole raw string as a level-0 key rather than losing it.
	return 0, raw
}

// DisplayKey returns the physical key sequence a raw mapping key names, with
// any capture/override level prefix stripped — the spelling to SHOW a user,
// since the keys they press are the same at every level. Reports false for a
// wildcard binding, which names no pressable key at all.
func DisplayKey(raw string) (string, bool) {
	_, seq := parseRawKey(raw)
	if seq == wildcardKey {
		return "", false
	}
	return seq, true
}

// rebuild derives the per-level view from rawMap. Called on every mutation;
// keymaps are small and switches are rare (focus changes), so clarity wins
// over incremental bookkeeping.
func (sp *SequenceProcessor) rebuild() {
	sp.levels = make(map[int]*levelBindings)
	sp.allKeys = make(map[string]bool)

	// Deterministic on duplicate (level, sequence) pairs spelled differently
	// ("capture capture X" vs "override X"): process raw keys in sorted
	// order, so the lexicographically-last spelling wins, every time.
	raws := make([]string, 0, len(sp.rawMap))
	for raw := range sp.rawMap {
		raws = append(raws, raw)
	}
	sort.Strings(raws)

	for _, raw := range raws {
		level, seq := parseRawKey(raw)
		lb := sp.levels[level]
		if lb == nil {
			lb = &levelBindings{specific: make(map[string]string)}
			sp.levels[level] = lb
		}
		if seq == wildcardKey {
			lb.wildcard = sp.rawMap[raw]
			lb.hasWild = true
			continue
		}
		lb.specific[seq] = sp.rawMap[raw]
		sp.allKeys[seq] = true
	}

	sp.levelOrder = sp.levelOrder[:0]
	for level := range sp.levels {
		sp.levelOrder = append(sp.levelOrder, level)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(sp.levelOrder)))

	sp.updateSequenceStarters()
}

// MapKey maps a key sequence to a command.
func (sp *SequenceProcessor) MapKey(keySequence, command string) {
	sp.rawMap[keySequence] = command
	sp.rebuild()
}

// UnmapKey removes a key mapping.
func (sp *SequenceProcessor) UnmapKey(keySequence string) {
	delete(sp.rawMap, keySequence)
	sp.rebuild()
}

// GetMapping returns the command mapped to a key sequence (in its raw
// spelling, level prefix included), or empty string if not found.
func (sp *SequenceProcessor) GetMapping(keySequence string) string {
	return sp.rawMap[keySequence]
}

// HelpTopic resolves the context-sensitive help topic for a key prefix: the
// value of the deepest "help" virtual binding matching activeSequence. It walks
// the prefix from full length down to the root, returning the first "<prefix>
// help" mapping found ("help" at the root); levels are searched highest first.
// Empty when no help binding applies. The "help" pseudo-key never fires as a
// command — it exists only to be looked up here — so it is excluded from
// completions.
func (sp *SequenceProcessor) HelpTopic(activeSequence string) string {
	var parts []string
	if activeSequence != "" {
		parts = strings.Split(activeSequence, " ")
	}
	for i := len(parts); i >= 0; i-- {
		var key string
		if i == 0 {
			key = "help"
		} else {
			key = strings.Join(parts[:i], " ") + " help"
		}
		if v, ok := sp.lookupSeqDirect(key); ok && v != "" {
			return unquoteMapping(v)
		}
	}
	return ""
}

// lookupSeqDirect finds a sequence's command by exact spelling, highest level
// first.
func (sp *SequenceProcessor) lookupSeqDirect(seq string) (string, bool) {
	for _, level := range sp.levelOrder {
		if lb := sp.levels[level]; lb != nil {
			if v, ok := lb.specific[seq]; ok {
				return v, true
			}
		}
	}
	return "", false
}

// unquoteMapping strips one layer of surrounding double quotes from a mapping
// value. Mapping values are usually written bare (window_prior), but a topic is
// naturally quoted (help ="keys"), and the config parser keeps the quotes — so
// the help topic would otherwise be the literal `"keys"`, not keys.
func unquoteMapping(v string) string {
	v = strings.TrimSpace(v)
	if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
		return v[1 : len(v)-1]
	}
	return v
}

// SetMappings replaces the entire keymap with a copy of m (used to switch the
// active mapping set when the focused window changes).
func (sp *SequenceProcessor) SetMappings(m map[string]string) {
	sp.rawMap = make(map[string]string, len(m))
	for k, v := range m {
		sp.rawMap[k] = v
	}
	sp.rebuild()
}

// GetAllMappings returns a copy of all key mappings, keyed by their raw
// spellings (level prefixes included — see DisplayKey for the user-facing
// form).
func (sp *SequenceProcessor) GetAllMappings() map[string]string {
	result := make(map[string]string, len(sp.rawMap))
	for k, v := range sp.rawMap {
		result[k] = v
	}
	return result
}

// updateSequenceStarters rebuilds the set of sequence starters from the
// parsed keymap. Starters are drawn from every level: a "capture ^B N" makes
// ^B a starter exactly as a level-0 "^B N" does.
func (sp *SequenceProcessor) updateSequenceStarters() {
	sp.sequenceStarters = make(map[string]bool)
	sp.controlSequenceStarters = make(map[string]bool)

	for key := range sp.allKeys {
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
//
// With an executor installed, the processor RUNS the resolved bindings itself
// — a key can resolve to a stack of candidates across levels, tried in
// precedence order until one takes it — and ProcessResult.Command is empty.
// With a nil executor it reports the top candidate instead (resolution-only
// mode; there is no status to descend on).
func (sp *SequenceProcessor) ProcessKey(key string) ProcessResult {
	// Don't process empty keys (likely timeout signals)
	if key == "" {
		return ProcessResult{Command: "", Handled: true}
	}

	// If we have an active sequence, handle accordingly
	if sp.activeSequence != "" {
		return sp.handleKeyWithActiveSequence(key)
	}

	// Rule 1: a key that could begin a mapped sequence is HELD, at any level,
	// wildcards notwithstanding — the chord in progress outranks every
	// single-key claim, and the wildcard only sees the starter if the
	// sequence later disqualifies (releaseKey).
	if sp.isSequenceStarter(key) {
		sp.activeSequence = key
		return ProcessResult{Command: "", Handled: true}
	}

	return sp.resolveKeyEvent(key)
}

// resolveKeyEvent runs one ordinary key (no sequence in play) through the
// level stack: candidates from the highest level down, then the default
// handling floor. The floor is load-bearing for the reclaim idiom — a
// `capture ^C = false` declines the capture so ^C falls through the levels
// and lands exactly where an uncaptured ^C always landed.
func (sp *SequenceProcessor) resolveKeyEvent(key string) ProcessResult {
	cands := sp.getCandidates(key)

	if sp.executor == nil {
		if len(cands) > 0 {
			return ProcessResult{Command: cands[0].command, Handled: true}
		}
		return ProcessResult{Command: sp.getDefaultHandling(key), Handled: true}
	}

	if !sp.runCandidates(key, cands) {
		if command := sp.getDefaultHandling(key); command != "" {
			sp.executor(key, command)
		}
	}
	return ProcessResult{Handled: true}
}

// getCandidates builds the precedence-ordered claims on a key event: one
// candidate per level, highest level first. Within a level the specific
// binding SHADOWS the wildcard — the wildcard is a level's answer only for
// the keys that level does not name — so declining a specific `capture ^C`
// drops past the capture level entirely rather than into its own net.
// Wildcards claim single keys only; sequences are always named.
func (sp *SequenceProcessor) getCandidates(seq string) []candidate {
	var cands []candidate
	single := !strings.Contains(seq, " ")

	for _, level := range sp.levelOrder {
		lb := sp.levels[level]
		if lb == nil {
			continue
		}
		if cmd, ok := lb.specific[seq]; ok {
			cands = append(cands, candidate{level: level, command: cmd, matchedSequence: seq})
			continue
		}
		found := false
		for _, fallback := range sp.getKeyFallbacks(seq) {
			if fallback == seq {
				continue
			}
			if cmd, ok := lb.specific[fallback]; ok {
				cands = append(cands, candidate{level: level, command: cmd, matchedSequence: fallback, isFallback: true})
				found = true
				break
			}
		}
		if found {
			continue
		}
		if single && lb.hasWild {
			cands = append(cands, candidate{level: level, command: lb.wildcard, wildcard: true, matchedSequence: seq})
		}
	}
	return cands
}

// primaryCandidate returns the index of the first specific (non-wildcard)
// candidate, or -1. The pending-match machinery keys on the specific match's
// spelling; a wildcard can neither begin nor extend a sequence.
func primaryCandidate(cands []candidate) int {
	for i, c := range cands {
		if !c.wildcard {
			return i
		}
	}
	return -1
}

// runCandidates executes candidates in order until one reports the key
// handled. Reports whether any did. A nil executor handles nothing.
func (sp *SequenceProcessor) runCandidates(key string, cands []candidate) bool {
	if sp.executor == nil {
		return false
	}
	for _, c := range cands {
		if sp.executor(key, c.command) {
			return true
		}
	}
	return false
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

	// Multi-part sequence handling: every part after the starter admits its
	// equivalent spellings (see partVariants), combined across ALL parts at
	// once — a continuation key must reach the mapping whichever spelling the
	// map uses, even when several parts are aliased simultaneously ("^O ^V C"
	// for a mapped "^O V ^C"). The as-pressed sequence enumerates first, so an
	// exact mapping always wins over an alias.
	if strings.Contains(sequence, " ") {
		parts := strings.Split(sequence, " ")
		isControlStarter := sp.controlSequenceStarters[parts[0]]

		seqs := []string{parts[0]}
		for i := 1; i < len(parts); i++ {
			variants := sp.partVariants(parts[i], isControlStarter)
			next := make([]string, 0, len(seqs)*len(variants))
			for _, prefix := range seqs {
				for _, v := range variants {
					next = append(next, prefix+" "+v)
				}
			}
			seqs = next
			if len(seqs) > 256 {
				seqs = seqs[:256] // runaway guard; real chords are 2-4 parts
			}
		}
		for _, s := range seqs {
			if s != sequence {
				fallbacks = append(fallbacks, s)
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

// partVariants returns the equivalent spellings of one continuation key token
// within a sequence, the token itself always first (so as-pressed outranks any
// alias). A single letter admits its case flip; within a control-started
// sequence it also admits its control form (c/C ~ ^C), and a control token or
// a named key with a control alias admits its plain letter in both cases
// (^C ~ C ~ c, return ~ ^M ~ M ~ m). This is what lets "^B C" entered as
// "^B ^C" complete the mapping mid-sequence instead of abandoning the prefix.
// Named-key aliases (esc/^[, back/^H, ...) at the sequence tail are handled by
// the alias suffix pass in getKeyFallbacks.
func (sp *SequenceProcessor) partVariants(part string, controlSeq bool) []string {
	out := []string{part}
	add := func(s string) {
		for _, e := range out {
			if e == s {
				return
			}
		}
		out = append(out, s)
	}
	if r := []rune(part); len(r) == 1 && unicode.IsLetter(r[0]) {
		if unicode.IsUpper(r[0]) {
			add(strings.ToLower(part))
		} else {
			add(strings.ToUpper(part))
		}
		if controlSeq {
			add("^" + strings.ToUpper(part)) // control keys are canonically ^X
		}
	}
	if controlSeq {
		if cv := sp.getSimpleControl(part); cv != "" {
			add(cv)
			if len(cv) == 2 && cv[0] == '^' {
				letter := string(cv[1])
				add(letter)
				add(strings.ToLower(letter))
			}
		}
	}
	return out
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
	for mappedKey := range sp.allKeys {
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
	for mappedKey := range sp.allKeys {
		if strings.HasPrefix(mappedKey, sequence) {
			return true
		}
	}

	// Check fallback prefixes
	for _, fallback := range sp.getKeyFallbacks(sequence) {
		if fallback == sequence {
			continue
		}
		for mappedKey := range sp.allKeys {
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
	for mappedKey := range sp.allKeys {
		if strings.HasPrefix(mappedKey, searchPrefix) {
			return true
		}
	}

	if matchedSequence != "" && matchedSequence != sequence {
		fallbackPrefix := matchedSequence + " "
		for mappedKey := range sp.allKeys {
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
		for mappedKey := range sp.allKeys {
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
	if cands := sp.getCandidates(fullSequence); len(cands) > 0 {
		matched := fullSequence
		isFallback := false
		if pi := primaryCandidate(cands); pi >= 0 {
			matched = cands[pi].matchedSequence
			isFallback = cands[pi].isFallback
		}
		if sp.hasLongerPotentialMatches(fullSequence, matched) {
			// Store as pending and wait for more input
			sp.pendingShortMatch = fullSequence
			if isFallback {
				sp.pendingFallbackMatch = matched
			} else {
				sp.pendingFallbackMatch = ""
			}
			sp.keyBuffer = nil
			sp.activeSequence = fullSequence
			return ProcessResult{Command: "", Handled: true}
		}

		// No longer matches possible: resolve now.
		sp.ClearActiveSequence()
		if sp.executor == nil {
			return ProcessResult{Command: cands[0].command, Handled: true}
		}
		// A MATCHED sequence is consumed whether or not a candidate takes it:
		// declining descends through the levels, but a sequence every level
		// declined does not unwind into the child or the buffer — ^B N
		// failing must not type "N".
		sp.runCandidates(key, cands)
		return ProcessResult{Command: "", Handled: true}
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

	// Invalid continuation: unwind. The starter is RELEASED — its sequence
	// came to nothing, so the capture layers get it (this is how a held ^B
	// reaches a hosted shell), else its literal self — and the middle keys
	// plus this one replay as ordinary keys.
	parts := strings.Split(sp.activeSequence, " ")
	sp.ClearActiveSequence()
	sp.releaseKey(parts[0])
	for i := 1; i < len(parts); i++ {
		sp.handleSingleKey(parts[i])
	}
	sp.handleSingleKey(key)
	return ProcessResult{Command: "", Handled: true}
}

// releaseKey lets go of a sequence starter whose sequence disqualified. The
// key was never an ordinary keystroke — it was held as a possible prefix — so
// it does not re-enter sequence consideration and its own specific bindings
// (which lost to the sequence the moment it was held) do not fire. The
// wildcards get first refusal, highest level down: in a pty viewport that is
// `capture * = tinput_key` sending the starter to the child as-is. Failing
// those it falls back to its pre-capture behavior: a single character inserts
// as text, a control or named key is simply dropped.
func (sp *SequenceProcessor) releaseKey(key string) {
	if sp.executor != nil {
		for _, level := range sp.levelOrder {
			if lb := sp.levels[level]; lb != nil && lb.hasWild {
				if sp.executor(key, lb.wildcard) {
					return
				}
			}
		}
	}
	if len(key) == 1 && sp.executor != nil {
		sp.executor(key, "insert '"+escapeStringLiteral(key)+"'")
	}
}

// processPendingMatch executes a pending match and reprocesses buffered keys.
func (sp *SequenceProcessor) processPendingMatch() {
	if sp.pendingShortMatch == "" {
		return
	}

	cands := sp.getCandidates(sp.pendingShortMatch)
	keysToReprocess := sp.keyBuffer
	parts := strings.Split(sp.pendingShortMatch, " ")
	lastKey := parts[len(parts)-1]

	sp.pendingShortMatch = ""
	sp.pendingFallbackMatch = ""
	sp.keyBuffer = nil
	sp.ClearActiveSequence()

	if len(cands) > 0 && sp.executor != nil {
		sp.runCandidates(lastKey, cands)

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

	sp.resolveKeyEvent(key)
}

// getDefaultHandling returns the default command for unmapped keys.
func (sp *SequenceProcessor) getDefaultHandling(key string) string {
	switch key {
	case "space":
		return "insert ' '"
	case "del", "back":
		return "nav_history_prior|del_char_prior"
	case "return":
		return "nav_follow false|accept|insert_newline"
	case "^C":
		return "cancel|buffer_close"
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

	for mappedKey := range sp.allKeys {
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
		for mappedKey := range sp.allKeys {
			if strings.HasPrefix(mappedKey, fallbackPrefix) {
				nextPart := strings.TrimPrefix(mappedKey, fallbackPrefix)
				nextKey := strings.Split(nextPart, " ")[0]
				completions[nextKey] = true
			}
		}
	}

	// "help" is a virtual binding (it names the Quick Help topic for this
	// prefix, resolved by HelpTopic), not a key the user can press — never
	// offer it as a completion.
	delete(completions, virtualHelpKey)

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
	for k, v := range sp.rawMap {
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
