package parser

import (
	"fmt"
	"regexp"
	"strings"
)

// CardType identifies the kind of flashcard extracted from a Rem.
type CardType string

const (
	CardTypeForward    CardType = "forward"
	CardTypeBackward   CardType = "backward"
	CardTypeDescriptor CardType = "descriptor"
	CardTypeList       CardType = "list"
	CardTypeCloze      CardType = "cloze"
)

// ParsedCard represents a flashcard generated from a Rem's content.
type ParsedCard struct {
	Type       CardType `json:"type"`
	Front      string   `json:"front"`
	Back       string   `json:"back"`
	ClozeIndex int      `json:"cloze_index,omitempty"`
	Hint       string   `json:"hint,omitempty"`
}

// ParsedReference represents a [[reference]] found in a Rem.
type ParsedReference struct {
	Raw         string `json:"raw"`
	TargetTitle string `json:"target_title"`
	Alias       string `json:"alias,omitempty"`
}

// ParsedPDFPin represents a PDF citation pin reference ([[pdf:...]] or [Label](pdf:...)).
type ParsedPDFPin struct {
	Raw         string `json:"raw"`
	DocID       string `json:"doc_id"`
	PageNumber  int    `json:"page_number"`
	HighlightID string `json:"highlight_id,omitempty"`
	Label       string `json:"label,omitempty"`
}

// ParseResult holds all extracted cards, references, and formatted text.
type ParseResult struct {
	Cards      []ParsedCard      `json:"cards"`
	References []ParsedReference `json:"references"`
	PDFPins    []ParsedPDFPin    `json:"pdf_pins,omitempty"`
	CleanText  string            `json:"clean_text"`
}

var (
	// Reference regex: [[title]] or [[title|alias]]
	refRegex = regexp.MustCompile(`\[\[(.*?)\]\]`)
	// Cloze regex: {{content}} where content can be "text", "c1::text", "text::hint", "c1::text::hint"
	clozeRegex = regexp.MustCompile(`\{\{(.*?)\}\}`)
	// Markdown PDF pin regex: [Label](pdf:doc_id#...)
	pdfMarkdownPinRegex = regexp.MustCompile(`\[([^\]]*)\]\(pdf:([^)]+)\)`)
)

func parsePDFPinParams(rawRef string) (docID string, pageNum int, hlID string) {
	pageNum = 1
	parts := strings.SplitN(rawRef, "#", 2)
	docID = strings.TrimSpace(parts[0])
	if len(parts) > 1 {
		hash := parts[1]
		for _, param := range strings.Split(hash, "&") {
			kv := strings.SplitN(param, "=", 2)
			if len(kv) == 2 {
				key := strings.ToLower(strings.TrimSpace(kv[0]))
				val := strings.TrimSpace(kv[1])
				switch key {
				case "p", "page":
					var p int
					if _, err := fmt.Sscanf(val, "%d", &p); err == nil && p > 0 {
						pageNum = p
					}
				case "h", "highlight":
					hlID = val
				}
			}
		}
	}
	return docID, pageNum, hlID
}

// ParseContent inspects a Rem's text and extracts flashcards and references.
func ParseContent(content string) ParseResult {
	trimmed := strings.TrimSpace(content)
	result := ParseResult{
		Cards:      make([]ParsedCard, 0),
		References: make([]ParsedReference, 0),
		PDFPins:    make([]ParsedPDFPin, 0),
		CleanText:  trimmed,
	}

	if trimmed == "" {
		return result
	}

	// 1. Extract References: [[Target]] or [[Target|Alias]]
	matches := refRegex.FindAllStringSubmatch(trimmed, -1)
	for _, m := range matches {
		if len(m) > 1 {
			inner := strings.TrimSpace(m[1])
			if inner != "" {
				// If reference is a PDF pin [[pdf:...]], do not treat as a note reference
				if strings.HasPrefix(strings.ToLower(inner), "pdf:") {
					rawRef := inner[4:]
					var label string
					if parts := strings.SplitN(rawRef, "|", 2); len(parts) == 2 {
						rawRef = parts[0]
						label = strings.TrimSpace(parts[1])
					}
					docID, pageNum, hlID := parsePDFPinParams(rawRef)
					if label == "" {
						label = fmt.Sprintf("p.%d", pageNum)
					}
					result.PDFPins = append(result.PDFPins, ParsedPDFPin{
						Raw:         m[0],
						DocID:       docID,
						PageNumber:  pageNum,
						HighlightID: hlID,
						Label:       label,
					})
					continue
				}

				var target, alias string
				if parts := strings.SplitN(inner, "|", 2); len(parts) == 2 {
					target = strings.TrimSpace(parts[0])
					alias = strings.TrimSpace(parts[1])
				} else {
					target = inner
				}
				result.References = append(result.References, ParsedReference{
					Raw:         m[0],
					TargetTitle: target,
					Alias:       alias,
				})
			}
		}
	}

	// 1b. Extract Markdown-style PDF Pins: [Label](pdf:...)
	mdMatches := pdfMarkdownPinRegex.FindAllStringSubmatch(trimmed, -1)
	for _, m := range mdMatches {
		if len(m) > 2 {
			label := strings.TrimSpace(m[1])
			rawRef := strings.TrimSpace(m[2])
			docID, pageNum, hlID := parsePDFPinParams(rawRef)
			if label == "" {
				label = fmt.Sprintf("p.%d", pageNum)
			}
			result.PDFPins = append(result.PDFPins, ParsedPDFPin{
				Raw:         m[0],
				DocID:       docID,
				PageNumber:  pageNum,
				HighlightID: hlID,
				Label:       label,
			})
		}
	}

	// 2. Extract Delimiter-based Flashcards (only outside of {{cloze}} or [[ref]])
	delim, pos := findTopLevelDelimiter(trimmed)
	if delim != "" {
		front := strings.TrimSpace(trimmed[:pos])
		back := strings.TrimSpace(trimmed[pos+len(delim):])
		cleanFront := CleanDelimiters(front)
		cleanBack := CleanDelimiters(back)

		if cleanFront != "" {
			switch delim {
			case ":::":
				if cleanBack != "" {
					result.Cards = append(result.Cards,
						ParsedCard{Type: CardTypeForward, Front: cleanFront, Back: cleanBack},
						ParsedCard{Type: CardTypeBackward, Front: cleanBack, Back: cleanFront},
					)
				}
			case "::":
				if cleanBack != "" {
					result.Cards = append(result.Cards,
						ParsedCard{Type: CardTypeForward, Front: cleanFront, Back: cleanBack},
					)
				}
			case ";;":
				if cleanBack != "" {
					result.Cards = append(result.Cards,
						ParsedCard{
							Type:  CardTypeDescriptor,
							Front: fmt.Sprintf("%s ;;", cleanFront),
							Back:  cleanBack,
						},
					)
				}
			case "==>":
				// Multi-line list card: back can be provided inline or populated from child bullets
				result.Cards = append(result.Cards,
					ParsedCard{
						Type:  CardTypeList,
						Front: fmt.Sprintf("%s ==>", cleanFront),
						Back:  cleanBack,
					},
				)
			}
		}
	}

	// 3. Extract Cloze Deletions
	clozeCards := parseClozes(trimmed)
	if len(clozeCards) > 0 {
		result.Cards = append(result.Cards, clozeCards...)
	}

	result.CleanText = CleanDelimiters(trimmed)
	return result
}

type clozeItem struct {
	raw   string
	index int
	text  string
	hint  string
	start int
	end   int
}

func parseClozes(content string) []ParsedCard {
	locs := clozeRegex.FindAllStringSubmatchIndex(content, -1)
	if len(locs) == 0 {
		return nil
	}

	var items []clozeItem
	autoIndex := 1

	for _, loc := range locs {
		raw := content[loc[0]:loc[1]]
		inner := content[loc[2]:loc[3]]
		inner = strings.TrimSpace(inner)
		if inner == "" {
			continue
		}

		cIdx := autoIndex
		text := inner
		hint := ""

		// Check for c1:: / c2:: prefix
		if strings.HasPrefix(inner, "c") || strings.HasPrefix(inner, "C") {
			colonIdx := strings.Index(inner, "::")
			if colonIdx != -1 {
				prefix := inner[:colonIdx]
				var num int
				if n, err := fmt.Sscanf(prefix[1:], "%d", &num); err == nil && n == 1 && num > 0 {
					cIdx = num
					text = strings.TrimSpace(inner[colonIdx+2:])
				}
			}
		}

		// Check for hint after answer: "text::hint"
		if hintIdx := strings.Index(text, "::"); hintIdx != -1 {
			hint = strings.TrimSpace(text[hintIdx+2:])
			text = strings.TrimSpace(text[:hintIdx])
		}

		items = append(items, clozeItem{
			raw:   raw,
			index: cIdx,
			text:  text,
			hint:  hint,
			start: loc[0],
			end:   loc[1],
		})
		autoIndex++
	}

	if len(items) == 0 {
		return nil
	}

	// Group items by index so that same index clozes (e.g. c1) are masked together
	indexGroups := make(map[int][]clozeItem)
	var orderedIndices []int
	for _, it := range items {
		if _, exists := indexGroups[it.index]; !exists {
			orderedIndices = append(orderedIndices, it.index)
		}
		indexGroups[it.index] = append(indexGroups[it.index], it)
	}

	var cards []ParsedCard
	for _, idx := range orderedIndices {
		var answers []string
		var hints []string

		// Build front by masking all clozes in targetGroup and unmasking other clozes
		var b strings.Builder
		lastPos := 0

		for _, it := range items {
			b.WriteString(content[lastPos:it.start])
			if it.index == idx {
				// Mask this cloze
				answers = append(answers, it.text)
				if it.hint != "" {
					hints = append(hints, it.hint)
					b.WriteString(fmt.Sprintf("[%s]", it.hint))
				} else {
					b.WriteString("[...]")
				}
			} else {
				// Other cloze: show regular text
				b.WriteString(it.text)
			}
			lastPos = it.end
		}
		b.WriteString(content[lastPos:])

		front := strings.TrimSpace(b.String())
		back := strings.Join(answers, ", ")
		combinedHint := strings.Join(hints, ", ")

		cards = append(cards, ParsedCard{
			Type:       CardTypeCloze,
			Front:      front,
			Back:       back,
			ClozeIndex: idx,
			Hint:       combinedHint,
		})
	}

	return cards
}

// CleanDelimiters removes delimiter syntax to create clean plain text.
func CleanDelimiters(text string) string {
	s := text
	// Replace [[title|alias]] or [[pdf:doc#...|label]] with label or alias
	s = refRegex.ReplaceAllStringFunc(s, func(m string) string {
		inner := strings.TrimSuffix(strings.TrimPrefix(m, "[["), "]]")
		if strings.HasPrefix(strings.ToLower(inner), "pdf:") {
			rawRef := inner[4:]
			if parts := strings.SplitN(rawRef, "|", 2); len(parts) == 2 {
				return strings.TrimSpace(parts[1])
			}
			_, pageNum, _ := parsePDFPinParams(rawRef)
			return fmt.Sprintf("p.%d", pageNum)
		}
		if parts := strings.SplitN(inner, "|", 2); len(parts) == 2 {
			return parts[1]
		}
		return inner
	})

	// Replace [label](pdf:doc#...) with label
	s = pdfMarkdownPinRegex.ReplaceAllStringFunc(s, func(m string) string {
		sub := pdfMarkdownPinRegex.FindStringSubmatch(m)
		if len(sub) > 2 {
			label := strings.TrimSpace(sub[1])
			if label != "" {
				return label
			}
			_, pageNum, _ := parsePDFPinParams(sub[2])
			return fmt.Sprintf("p.%d", pageNum)
		}
		return m
	})

	// Replace {{c1::text::hint}} or {{text}} with text
	s = clozeRegex.ReplaceAllStringFunc(s, func(m string) string {
		inner := strings.TrimSuffix(strings.TrimPrefix(m, "{{"), "}}")
		if strings.HasPrefix(inner, "c") || strings.HasPrefix(inner, "C") {
			if colonIdx := strings.Index(inner, "::"); colonIdx != -1 {
				inner = inner[colonIdx+2:]
			}
		}
		if hintIdx := strings.Index(inner, "::"); hintIdx != -1 {
			inner = inner[:hintIdx]
		}
		return strings.TrimSpace(inner)
	})

	return strings.TrimSpace(s)
}

// findTopLevelDelimiter looks for flashcard delimiters outside of {{cloze}} or [[reference]] brackets.
func findTopLevelDelimiter(s string) (delim string, index int) {
	inCloze := 0
	inRef := 0
	n := len(s)

	for i := 0; i < n; i++ {
		// Track brackets
		if i+1 < n && s[i] == '{' && s[i+1] == '{' {
			inCloze++
			i++
			continue
		}
		if i+1 < n && s[i] == '}' && s[i+1] == '}' {
			if inCloze > 0 {
				inCloze--
			}
			i++
			continue
		}
		if i+1 < n && s[i] == '[' && s[i+1] == '[' {
			inRef++
			i++
			continue
		}
		if i+1 < n && s[i] == ']' && s[i+1] == ']' {
			if inRef > 0 {
				inRef--
			}
			i++
			continue
		}

		// Only match delimiters when at top level
		if inCloze == 0 && inRef == 0 {
			if strings.HasPrefix(s[i:], ":::") {
				return ":::", i
			}
			if strings.HasPrefix(s[i:], "::") {
				// Avoid matching C++/Rust scope resolution operators like std::vector
				if i > 0 && isIdentByte(s[i-1]) && i+2 < n && isIdentByte(s[i+2]) {
					// Mid-word scope operator, skip
				} else {
					return "::", i
				}
			}
			if strings.HasPrefix(s[i:], ";;") {
				return ";;", i
			}
			if strings.HasPrefix(s[i:], "==>") {
				return "==>", i
			}
		}
	}

	return "", -1
}

func isIdentByte(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '_'
}

