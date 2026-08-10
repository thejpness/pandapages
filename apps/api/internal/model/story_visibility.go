package model

// StoryVisibility determines which adult accounts may access a story before
// Reader release and Reading Level resolution begins.
type StoryVisibility string

const (
	StoryVisibilityPublic  StoryVisibility = "public"
	StoryVisibilityPrivate StoryVisibility = "private"
)

func ValidStoryVisibility(visibility StoryVisibility) bool {
	return visibility == StoryVisibilityPublic || visibility == StoryVisibilityPrivate
}
