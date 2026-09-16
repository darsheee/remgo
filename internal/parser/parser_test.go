package parser

import (
	"testing"
)

func TestForwardCard(t *testing.T) {
	input := "Golang :: A compiled, statically typed programming language designed at Google"
	res := ParseContent(input)

	if len(res.Cards) != 1 {
		t.Fatalf("expected 1 card, got %d", len(res.Cards))
	}
	c := res.Cards[0]
	if c.Type != CardTypeForward {
		t.Errorf("expected CardTypeForward, got %s", c.Type)
	}
	if c.Front != "Golang" {
		t.Errorf("expected Front 'Golang', got '%s'", c.Front)
	}
	if c.Back != "A compiled, statically typed programming language designed at Google" {
		t.Errorf("expected Back definition, got '%s'", c.Back)
	}
}

func TestTwoWayCard(t *testing.T) {
	input := "Hola ::: Hello"
	res := ParseContent(input)

	if len(res.Cards) != 2 {
		t.Fatalf("expected 2 cards, got %d", len(res.Cards))
	}

	c1 := res.Cards[0]
	if c1.Type != CardTypeForward || c1.Front != "Hola" || c1.Back != "Hello" {
		t.Errorf("card 1 mismatch: %+v", c1)
	}

	c2 := res.Cards[1]
	if c2.Type != CardTypeBackward || c2.Front != "Hello" || c2.Back != "Hola" {
		t.Errorf("card 2 mismatch: %+v", c2)
	}
}

func TestDescriptorCard(t *testing.T) {
	input := "Mitochondria ;; Powerhouse of the cell"
	res := ParseContent(input)

	if len(res.Cards) != 1 {
		t.Fatalf("expected 1 card, got %d", len(res.Cards))
	}
	c := res.Cards[0]
	if c.Type != CardTypeDescriptor {
		t.Errorf("expected CardTypeDescriptor, got %s", c.Type)
	}
	if c.Front != "Mitochondria ;;" {
		t.Errorf("expected Front 'Mitochondria ;;', got '%s'", c.Front)
	}
	if c.Back != "Powerhouse of the cell" {
		t.Errorf("expected Back 'Powerhouse of the cell', got '%s'", c.Back)
	}
}

func TestListCard(t *testing.T) {
	input := "Primary colors ==> Red, Green, Blue"
	res := ParseContent(input)

	if len(res.Cards) != 1 {
		t.Fatalf("expected 1 card, got %d", len(res.Cards))
	}
	c := res.Cards[0]
	if c.Type != CardTypeList {
		t.Errorf("expected CardTypeList, got %s", c.Type)
	}
	if c.Front != "Primary colors ==>" {
		t.Errorf("expected Front 'Primary colors ==>', got '%s'", c.Front)
	}
	if c.Back != "Red, Green, Blue" {
		t.Errorf("expected Back 'Red, Green, Blue', got '%s'", c.Back)
	}
}

func TestSingleCloze(t *testing.T) {
	input := "The speed of light is {{299,792,458}} m/s"
	res := ParseContent(input)

	if len(res.Cards) != 1 {
		t.Fatalf("expected 1 cloze card, got %d", len(res.Cards))
	}
	c := res.Cards[0]
	if c.Type != CardTypeCloze {
		t.Errorf("expected CardTypeCloze, got %s", c.Type)
	}
	if c.Front != "The speed of light is [...] m/s" {
		t.Errorf("unexpected Front: '%s'", c.Front)
	}
	if c.Back != "299,792,458" {
		t.Errorf("unexpected Back: '%s'", c.Back)
	}
}

func TestClozeWithHint(t *testing.T) {
	input := "The capital of France is {{Paris::city}}"
	res := ParseContent(input)

	if len(res.Cards) != 1 {
		t.Fatalf("expected 1 cloze card, got %d", len(res.Cards))
	}
	c := res.Cards[0]
	if c.Front != "The capital of France is [city]" {
		t.Errorf("unexpected Front with hint: '%s'", c.Front)
	}
	if c.Back != "Paris" {
		t.Errorf("unexpected Back: '%s'", c.Back)
	}
	if c.Hint != "city" {
		t.Errorf("unexpected Hint: '%s'", c.Hint)
	}
}

func TestMultipleNumberedClozes(t *testing.T) {
	input := "{{c1::George Washington}} was the {{c2::1st::number}} President of the United States."
	res := ParseContent(input)

	if len(res.Cards) != 2 {
		t.Fatalf("expected 2 cloze cards, got %d", len(res.Cards))
	}

	// Card 1
	c1 := res.Cards[0]
	if c1.Front != "[...] was the 1st President of the United States." {
		t.Errorf("card 1 Front mismatch: '%s'", c1.Front)
	}
	if c1.Back != "George Washington" {
		t.Errorf("card 1 Back mismatch: '%s'", c1.Back)
	}

	// Card 2
	c2 := res.Cards[1]
	if c2.Front != "George Washington was the [number] President of the United States." {
		t.Errorf("card 2 Front mismatch: '%s'", c2.Front)
	}
	if c2.Back != "1st" {
		t.Errorf("card 2 Back mismatch: '%s'", c2.Back)
	}
}

func TestSharedIndexClozes(t *testing.T) {
	input := "{{c1::Hydrogen}} and {{c1::Oxygen}} form water."
	res := ParseContent(input)

	if len(res.Cards) != 1 {
		t.Fatalf("expected 1 shared cloze card, got %d", len(res.Cards))
	}
	c := res.Cards[0]
	if c.Front != "[...] and [...] form water." {
		t.Errorf("shared front mismatch: '%s'", c.Front)
	}
	if c.Back != "Hydrogen, Oxygen" {
		t.Errorf("shared back mismatch: '%s'", c.Back)
	}
}

func TestReferences(t *testing.T) {
	input := "Learn [[Computer Science]] and [[Operating Systems|OS internals]]"
	res := ParseContent(input)

	if len(res.References) != 2 {
		t.Fatalf("expected 2 references, got %d", len(res.References))
	}

	r1 := res.References[0]
	if r1.TargetTitle != "Computer Science" || r1.Alias != "" {
		t.Errorf("ref 1 mismatch: %+v", r1)
	}

	r2 := res.References[1]
	if r2.TargetTitle != "Operating Systems" || r2.Alias != "OS internals" {
		t.Errorf("ref 2 mismatch: %+v", r2)
	}
}

func TestCleanDelimiters(t *testing.T) {
	input := "Learn [[Go|Golang]] with {{c1::interfaces}} and [[Concurrency]]"
	cleaned := CleanDelimiters(input)
	expected := "Learn Golang with interfaces and Concurrency"
	if cleaned != expected {
		t.Errorf("CleanDelimiters expected '%s', got '%s'", expected, cleaned)
	}
}

func TestEmptyAndBoundaryCases(t *testing.T) {
	res1 := ParseContent("")
	if len(res1.Cards) != 0 || len(res1.References) != 0 {
		t.Errorf("empty input should produce no cards or refs")
	}

	res2 := ParseContent("   ")
	if len(res2.Cards) != 0 {
		t.Errorf("whitespace input should produce no cards")
	}

	res3 := ParseContent("Just a plain note without any delimiters")
	if len(res3.Cards) != 0 || len(res3.References) != 0 {
		t.Errorf("plain note should produce no cards, got %d", len(res3.Cards))
	}
}
