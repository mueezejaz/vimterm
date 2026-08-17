package keybind

// Action identifies a named application behavior. The configuration maps key
// sequences to action names; the application binds each name to a function.
type Action string

const (
	ActionMoveLeft      Action = "move_left"
	ActionMoveRight     Action = "move_right"
	ActionMoveUp        Action = "move_up"
	ActionMoveDown      Action = "move_down"
	ActionScrollUp      Action = "scroll_up"
	ActionScrollDown    Action = "scroll_down"
	ActionGotoTop       Action = "goto_top"
	ActionGotoBottom    Action = "goto_bottom"
	ActionEnterInsert   Action = "enter_insert"
	ActionEnterNormal   Action = "enter_normal"
	ActionSearchForward Action = "search_forward"
	ActionSearchNext    Action = "search_next"
	ActionSearchPrev    Action = "search_prev"
	ActionCommandPrompt Action = "command_prompt"
	ActionEnterVisual   Action = "enter_visual"
	ActionEnterVisLine  Action = "enter_visual_line"
	ActionCancelVisual  Action = "cancel_visual"
	ActionYank          Action = "yank"
	ActionRecordMacro   Action = "record_macro"
	ActionPlayMacro     Action = "play_macro"
	ActionRepeatLast    Action = "repeat_last"
	ActionFindChar      Action = "find_char"
	ActionFindCharBack  Action = "find_char_back"
	ActionFindUntil     Action = "find_until"
	ActionFindUntilBack Action = "find_until_back"
	ActionFindNext      Action = "find_next"
	ActionFindPrev      Action = "find_prev"
	ActionMoveWord      Action = "move_word"
	ActionMoveWordBack  Action = "move_word_back"
	ActionMoveWordEnd   Action = "move_word_end"
	ActionMoveWORD      Action = "move_word_upper"
	ActionMoveWORDBack  Action = "move_word_back_upper"
	ActionMoveWORDEnd   Action = "move_word_end_upper"
	ActionEnterInsertAfter Action = "enter_insert_after"
	ActionEnterInsertEnd   Action = "enter_insert_end"
	ActionEnterInsertHome  Action = "enter_insert_home"
	ActionPaste            Action = "paste"
	ActionPasteBefore      Action = "paste_before"
	ActionQuit             Action = "quit"
)

// AllActions lists every action the application understands. Config bindings
// to any other name are rejected at load time.
var AllActions = []Action{
	ActionMoveLeft,
	ActionMoveRight,
	ActionMoveUp,
	ActionMoveDown,
	ActionScrollUp,
	ActionScrollDown,
	ActionGotoTop,
	ActionGotoBottom,
	ActionEnterInsert,
	ActionEnterNormal,
	ActionSearchForward,
	ActionSearchNext,
	ActionSearchPrev,
	ActionCommandPrompt,
	ActionEnterVisual,
	ActionEnterVisLine,
	ActionCancelVisual,
	ActionYank,
	ActionRecordMacro,
	ActionPlayMacro,
	ActionRepeatLast,
	ActionFindChar,
	ActionFindCharBack,
	ActionFindUntil,
	ActionFindUntilBack,
	ActionFindNext,
	ActionFindPrev,
	ActionMoveWord,
	ActionMoveWordBack,
	ActionMoveWordEnd,
	ActionMoveWORD,
	ActionMoveWORDBack,
	ActionMoveWORDEnd,
	ActionEnterInsertAfter,
	ActionEnterInsertEnd,
	ActionEnterInsertHome,
	ActionPaste,
	ActionPasteBefore,
	ActionQuit,
}

// IsKnownAction reports whether a is a recognized action name.
func IsKnownAction(a Action) bool {
	for _, known := range AllActions {
		if known == a {
			return true
		}
	}
	return false
}

// kmNode is a trie node keyed by Key.
type kmNode struct {
	children  map[Key]*kmNode
	action    Action
	hasAction bool
}

// Keymap maps key sequences to actions for a single mode.
type Keymap struct {
	root *kmNode
}

// NewKeymap creates an empty keymap.
func NewKeymap() *Keymap {
	return &Keymap{root: &kmNode{children: make(map[Key]*kmNode)}}
}

// Bind associates a sequence of keys with an action.
func (km *Keymap) Bind(seq []Key, a Action) {
	n := km.root
	for _, k := range seq {
		child, ok := n.children[k]
		if !ok {
			child = &kmNode{children: make(map[Key]*kmNode)}
			n.children[k] = child
		}
		n = child
	}
	n.action = a
	n.hasAction = true
}

// Lookup returns the action bound to the exact sequence, if any.
func (km *Keymap) Lookup(seq []Key) (Action, bool) {
	n := km.root
	for _, k := range seq {
		child, ok := n.children[k]
		if !ok {
			return "", false
		}
		n = child
	}
	if !n.hasAction {
		return "", false
	}
	return n.action, true
}

// IsPrefix reports whether seq is a prefix of at least one bound sequence.
func (km *Keymap) IsPrefix(seq []Key) bool {
	n := km.root
	for _, k := range seq {
		child, ok := n.children[k]
		if !ok {
			return false
		}
		n = child
	}
	return len(n.children) > 0
}