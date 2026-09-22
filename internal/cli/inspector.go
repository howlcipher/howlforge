package cli

import "github.com/howlcipher/howlforge/internal/resume"

// resumeInspectorAvailable reports whether git based resume checking is
// possible. It is a separate function so the doctor command does not need to
// know how the inspector is constructed.
func resumeInspectorAvailable() (bool, error) {
	inspector, err := resume.NewInspector()
	if err != nil {
		return false, err
	}
	return inspector != nil, nil
}
