package reviewagent

import "errors"

func NormalizeIntensity(value string) (string, error) {
	if value == "" {
		return "medium", nil
	}
	switch value {
	case "low", "medium", "high":
		return value, nil
	default:
		return "", errors.New("Choose Quick, Balanced, or Thorough review intensity")
	}
}

func IntensityInstructions(intensity string) string {
	switch intensity {
	case "low":
		return "Quick review: prioritize high-impact correctness and safety regressions. Read the source needed to verify those concerns, then finish with a concise summary. Avoid speculative investigation."
	case "high":
		return "Thorough review: examine changed behavior, relevant callers and dependencies, error paths, boundary cases, and test coverage. Use read_review_file to verify cross-file assumptions. Prioritize concrete findings; extra review depth must not produce speculative or style-only comments."
	default:
		return "Balanced review: check changed behavior and relevant surrounding source, including likely error paths and missing regression coverage. Balance investigation depth with a concise, actionable review."
	}
}

// Never send an effort absent from the live model catalog. A model without
// configurable reasoning still receives the selected review-depth instructions.
func ModelEffort(models []Model, selected, intensity string) string {
	for _, model := range models {
		if (selected != "" && model.ID == selected) || (selected == "" && model.Default) {
			for _, effort := range model.ReasoningEfforts {
				if effort == intensity {
					return effort
				}
			}
			// Fixed-effort models and models exposing only specialized effort
			// levels retain their own default instead of rejecting the review.
			return ""
		}
	}
	return ""
}
