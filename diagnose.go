package preflight

import (
	"errors"
	"strings"
)

// Diagnosis turns a runtime failure into something the reader can act on.
//
// Preflight answers "is this deployment sound" at startup. This answers the
// other half: when something fails later, what kind of failure is it and what
// should be done about it. The two share a purpose -- neither is finished
// until it has said what to do -- and Advice is the shape of that answer.
type Diagnosis struct {
	// Kind classifies the failure, so a UI can branch on it without parsing
	// a message.
	Kind string

	// Error is the underlying error's text, kept verbatim.
	Error string

	Advice
}

// Advice is what to do about a class of failure.
//
// CheckCommand and FixCommand are separate on purpose. Someone diagnosing a
// problem on a machine they do not own needs to be able to look before they
// touch, and a single "run this" field forces the author to choose between
// giving a safe command and giving a useful one. Splitting them means the
// read-only one can always be offered first.
type Advice struct {
	// Suggestion is a sentence: what is likely wrong, in prose.
	Suggestion string

	// CheckCommand inspects, and changes nothing. Safe to run anywhere.
	CheckCommand string

	// FixCommand changes something. It may restart services or rewrite
	// configuration, and a UI should present it as an action rather than
	// as information.
	FixCommand string
}

// Error carries a Kind alongside an error, so a failure can be classified
// where it happens rather than guessed at later from its text.
type Error struct {
	Kind string
	Err  error
}

// Error implements error.
func (e *Error) Error() string {
	if e == nil || e.Err == nil {
		return ""
	}
	return e.Err.Error()
}

// Unwrap returns the wrapped error.
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// Wrap tags err with a kind. It returns nil when err is nil, so it can be
// applied to a result without a preceding check.
func Wrap(kind string, err error) error {
	if err == nil {
		return nil
	}
	return &Error{Kind: kind, Err: err}
}

// KindOf returns the kind carried by err, or "" when it carries none.
func KindOf(err error) string {
	var e *Error
	if errors.As(err, &e) && e != nil {
		return e.Kind
	}
	return ""
}

// Rule matches a class of failure and says what to do about it.
type Rule struct {
	// Kind is the classification this rule assigns.
	Kind string

	// Substrings matches an error whose text contains any of these,
	// lower-cased. Used only when the error carries no Kind of its own.
	Substrings []string

	// Advice is what to tell the reader.
	Advice
}

// Classifier maps errors to advice.
//
// Kind is matched first, and substrings only as a fallback. That order is
// deliberate: an error tagged at the point of failure knows what it is, while
// matching on text is guesswork that goes wrong quietly -- it survives until
// someone rewords a message or a library is updated, and then silently starts
// classifying everything as the fallback. Keeping it as the second-choice path
// means existing call sites keep working while new ones are tagged properly.
type Classifier struct {
	Rules []Rule

	// Fallback is used when nothing matches. A classifier with no fallback
	// still returns a Diagnosis, just without advice.
	Fallback Advice

	// FallbackKind is the Kind given to unmatched errors. Empty means
	// "unknown".
	FallbackKind string
}

// DefaultFallbackKind is the Kind assigned when nothing matches.
const DefaultFallbackKind = "unknown"

// Diagnose classifies err and returns the advice for it.
func (c Classifier) Diagnose(err error) Diagnosis {
	if err == nil {
		return Diagnosis{Kind: c.fallbackKind(), Advice: c.Fallback}
	}

	d := Diagnosis{Error: err.Error()}

	if kind := KindOf(err); kind != "" {
		for _, r := range c.Rules {
			if r.Kind == kind {
				d.Kind, d.Advice = kind, r.Advice
				return d
			}
		}
		// Tagged with a kind the classifier does not know: keep the kind,
		// since the caller meant something by it, but admit to having no
		// advice rather than inventing some.
		d.Kind, d.Advice = kind, c.Fallback
		return d
	}

	msg := strings.ToLower(err.Error())
	for _, r := range c.Rules {
		for _, sub := range r.Substrings {
			if sub != "" && strings.Contains(msg, strings.ToLower(sub)) {
				d.Kind, d.Advice = r.Kind, r.Advice
				return d
			}
		}
	}

	d.Kind, d.Advice = c.fallbackKind(), c.Fallback
	return d
}

func (c Classifier) fallbackKind() string {
	if c.FallbackKind == "" {
		return DefaultFallbackKind
	}
	return c.FallbackKind
}
