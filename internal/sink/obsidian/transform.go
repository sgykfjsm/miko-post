package obsidian

import (
	"strings"
	"time"
)

// lineBreakMarkup is what a line feed inside the message becomes.
//
// Literal text, not an escape: the note is Markdown, and Obsidian renders
// `<br>` as a break inside a paragraph. That is what keeps a multi-line message
// one physical line in the file (SC-010) while still reading as multiple lines
// in the vault.
const lineBreakMarkup = "<br>"

// entryPrefix is the list-item marker every entry starts with.
//
// The daily note is a Markdown document and each captured thought is a bullet
// in it, so the line has to begin as a list item or it merges into whatever
// paragraph precedes it.
const entryPrefix = "- "

// Entry renders one message as the single physical line to append (FR-047,
// FR-048, SC-010).
//
// The four steps are performed in the order contracts/obsidian-sink.md declares
// normative, and the order is not incidental:
//
//  1. \r\n becomes \n, then a bare \r becomes \n.
//  2. Every remaining \n becomes the literal <br>.
//  3. Prefix "- ", the local time, and one space.
//  4. Terminate with exactly one \n.
//
// Step 1 must precede step 2. Reversed, a \r\n pasted from another application
// — a Windows editor, a chat client, a web page — yields two <br> where one was
// meant, so the same text stored from two sources would produce two different
// lines. Doing it in this order makes the stored entry independent of where the
// text was copied from, which is the property the contract is protecting rather
// than the mechanics of the replacement.
//
// The message is otherwise untouched. No trimming, no width normalisation, no
// escaping of Markdown metacharacters: FR-011 delivers what the user typed, and
// a message that happens to contain `*` or `#` renders as Markdown in the
// vault, which is a note-taking application behaving as intended rather than a
// defect to defend against.
//
// The instant is a parameter for the same reason as in DailyNotePath: the time
// in the entry prefix and the note the entry lands in must come from one
// reading of the clock.
func Entry(message string, at time.Time, timeFormat string) string {
	// Step 1. Two passes, longest sequence first, so the \r of a \r\n is not
	// converted on its own and then leave the \n behind as a second break.
	normalized := strings.ReplaceAll(message, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")

	// Step 2.
	single := strings.ReplaceAll(normalized, "\n", lineBreakMarkup)

	// Steps 3 and 4. Exactly one trailing \n, whatever the message ended with:
	// its own trailing newlines became <br> in step 2, so they cannot add a
	// second physical line here.
	return entryPrefix + at.Format(timeFormat) + " " + single + "\n"
}
