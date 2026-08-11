package copyrighteligibility

import "strings"

// UKKnownException is a finite, policy-owned registry of surviving rights
// which Panda Pages can positively identify. A no-match means only that the
// work did not match this registry; it is not a legal conclusion about every
// possible historic, statutory, or prerogative right. Policy v2 remains a
// narrow automatic screen for ordinary literary works, not an exhaustive UK
// rights register.
type UKKnownException string

const (
	UKKnownExceptionNone               UKKnownException = ""
	UKKnownExceptionPeterPan           UKKnownException = "peter_pan"
	UKKnownExceptionKingJamesBible     UKKnownException = "king_james_bible"
	UKKnownExceptionBookOfCommonPrayer UKKnownException = "book_of_common_prayer"
)

// knownUKException applies exact normalized work identities only. The policy
// is intentionally not a fuzzy title matcher and never relies on a source
// provider identifier. CDPA 1988 s.301 identifies Peter Pan; current National
// Archives guidance identifies the King James Bible and Book of Common Prayer
// as Royal-prerogative protected works.
func knownUKException(title, author string) UKKnownException {
	normalizedTitle := normalizeKnownExceptionIdentity(title)

	if normalizedTitle == "peterpan" && knownPeterPanAuthor(author) {
		return UKKnownExceptionPeterPan
	}
	if knownKingJamesBibleTitle(normalizedTitle) {
		return UKKnownExceptionKingJamesBible
	}
	if normalizedTitle == "bookofcommonprayer" || normalizedTitle == "thebookofcommonprayer" {
		return UKKnownExceptionBookOfCommonPrayer
	}
	return UKKnownExceptionNone
}

func knownPeterPanAuthor(value string) bool {
	for _, candidate := range knownExceptionPersonNames(value) {
		switch candidate {
		case "jmbarrie", "jamesmatthewbarrie", "sirjamesmatthewbarrie":
			return true
		}
	}
	return false
}

func knownKingJamesBibleTitle(title string) bool {
	switch title {
	case "kingjamesbible", "thekingjamesbible", "kingjamesversion", "thekingjamesversion", "authorisedversion", "authorizedversion", "authorisedversionofthebible", "authorizedversionofthebible":
		return true
	default:
		return false
	}
}

func knownExceptionReason(exception UKKnownException) ReasonCode {
	switch exception {
	case UKKnownExceptionPeterPan:
		return ReasonUKKnownExceptionPeterPan
	case UKKnownExceptionKingJamesBible:
		return ReasonUKKnownExceptionKingJamesBible
	case UKKnownExceptionBookOfCommonPrayer:
		return ReasonUKKnownExceptionBookOfCommonPrayer
	default:
		return ""
	}
}

func normalizeKnownExceptionIdentity(value string) string {
	var normalized strings.Builder
	for _, character := range strings.ToLower(strings.TrimSpace(value)) {
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') {
			normalized.WriteRune(character)
		}
	}
	return normalized.String()
}

func knownExceptionPersonNames(value string) []string {
	values := []string{normalizeKnownExceptionPersonName(value)}
	withoutParenthetical := value
	if start := strings.Index(withoutParenthetical, "("); start >= 0 {
		if end := strings.Index(withoutParenthetical[start:], ")"); end >= 0 {
			withoutParenthetical = withoutParenthetical[:start] + withoutParenthetical[start+end+1:]
		}
	}
	parts := strings.Split(withoutParenthetical, ",")
	if len(parts) >= 2 {
		values = append(values, normalizeKnownExceptionPersonName(strings.TrimSpace(parts[1])+" "+strings.TrimSpace(parts[0])))
	}
	return values
}

func normalizeKnownExceptionPersonName(value string) string {
	var normalized strings.Builder
	for _, character := range strings.ToLower(strings.TrimSpace(value)) {
		if character >= 'a' && character <= 'z' {
			normalized.WriteRune(character)
		}
	}
	return normalized.String()
}
