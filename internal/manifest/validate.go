package manifest

import (
	"strings"

	"golang.org/x/mod/semver"
)

var (
	validOnLimit   = map[string]struct{}{"halt": {}, "escalate": {}}
	validOnInvalid = map[string]struct{}{"retry": {}, "repair": {}, "escalate": {}, "fail": {}}
	validOutputFmt = map[string]struct{}{"text": {}, "json": {}}
	validReasoning = map[string]struct{}{"low": {}, "medium": {}, "high": {}}
)

// Validate checks structural and semantic rules for a parsed Agent document.
func Validate(agent *Agent) error {
	if agent == nil {
		return ValidationErrors{{Path: "", Message: "manifest is nil"}}
	}
	var errs ValidationErrors

	if agent.APIVersion != APIVersionV1 {
		errs = append(errs, FieldError{
			Path:    "apiVersion",
			Message: "must be " + APIVersionV1,
		})
	}
	if agent.Kind != KindAgent {
		errs = append(errs, FieldError{
			Path:    "kind",
			Message: "must be " + KindAgent,
		})
	}

	errs = append(errs, validateMetadata(&agent.Metadata)...)
	errs = append(errs, validateSpec(&agent.Spec)...)
	if agent.Output != nil {
		errs = append(errs, validateOutput(agent.Output)...)
	}

	if len(errs) > 0 {
		return errs
	}
	return nil
}

func validateMetadata(m *AgentMetadata) []FieldError {
	var errs []FieldError
	if strings.TrimSpace(m.Name) == "" {
		errs = append(errs, FieldError{Path: "metadata.name", Message: "is required"})
	}
	if strings.TrimSpace(m.Namespace) == "" {
		errs = append(errs, FieldError{Path: "metadata.namespace", Message: "is required"})
	}
	if strings.TrimSpace(m.Version) == "" {
		errs = append(errs, FieldError{Path: "metadata.version", Message: "is required"})
	} else if !isValidSemver(m.Version) {
		errs = append(errs, FieldError{Path: "metadata.version", Message: "must be valid semver"})
	}
	return errs
}

func validateSpec(spec *AgentSpec) []FieldError {
	var errs []FieldError
	if strings.TrimSpace(spec.Purpose) == "" {
		errs = append(errs, FieldError{Path: "spec.purpose", Message: "is required"})
	}
	errs = append(errs, validateInstructions(&spec.Instructions)...)
	errs = append(errs, validateModel(&spec.Model)...)
	if spec.Limits != nil {
		errs = append(errs, validateLimits(spec.Limits)...)
	}
	return errs
}

func validateInstructions(in *InstructionsSpec) []FieldError {
	hasRef := strings.TrimSpace(in.Ref) != ""
	hasText := strings.TrimSpace(in.Text) != ""
	switch {
	case hasRef && hasText:
		return []FieldError{{
			Path:    "spec.instructions",
			Message: "set exactly one of ref or text, not both",
		}}
	case !hasRef && !hasText:
		return []FieldError{{
			Path:    "spec.instructions",
			Message: "must set exactly one of ref or text",
		}}
	}
	return nil
}

func validateModel(m *ModelConfig) []FieldError {
	var errs []FieldError
	if strings.TrimSpace(m.Provider) == "" {
		errs = append(errs, FieldError{Path: "spec.model.provider", Message: "is required"})
	}
	if strings.TrimSpace(m.Name) == "" {
		errs = append(errs, FieldError{Path: "spec.model.name", Message: "is required"})
	}
	if m.Parameters != nil {
		errs = append(errs, validateModelParameters(m.Parameters)...)
	}
	if m.Reasoning != nil && m.Reasoning.Effort != "" {
		if _, ok := validReasoning[m.Reasoning.Effort]; !ok {
			errs = append(errs, FieldError{
				Path:    "spec.model.reasoning.effort",
				Message: "must be one of low, medium, high",
			})
		}
	}
	return errs
}

func validateModelParameters(p *ModelParameters) []FieldError {
	if p.Temperature != nil && p.TopP != nil {
		return []FieldError{{
			Path:    "spec.model.parameters",
			Message: "set temperature or top_p, not both",
		}}
	}
	return nil
}

func validateLimits(l *Limits) []FieldError {
	if l.OnLimit == "" {
		return nil
	}
	if _, ok := validOnLimit[l.OnLimit]; !ok {
		return []FieldError{{
			Path:    "spec.limits.on_limit",
			Message: "must be one of halt, escalate",
		}}
	}
	return nil
}

func validateOutput(out *OutputSpec) []FieldError {
	var errs []FieldError
	if out.Format != "" {
		if _, ok := validOutputFmt[out.Format]; !ok {
			errs = append(errs, FieldError{
				Path:    "output.format",
				Message: "must be one of text, json",
			})
		}
	}
	if out.Schema != nil {
		errs = append(errs, validateSchema(out.Schema)...)
	}
	if out.OnInvalid != "" {
		if _, ok := validOnInvalid[out.OnInvalid]; !ok {
			errs = append(errs, FieldError{
				Path:    "output.on_invalid",
				Message: "must be one of retry, repair, escalate, fail",
			})
		}
	}
	return errs
}

func validateSchema(s *SchemaSpec) []FieldError {
	hasRef := strings.TrimSpace(s.Ref) != ""
	hasInline := len(s.Inline) > 0
	switch {
	case hasRef && hasInline:
		return []FieldError{{
			Path:    "output.schema",
			Message: "set exactly one of ref or inline, not both",
		}}
	case !hasRef && !hasInline:
		return []FieldError{{
			Path:    "output.schema",
			Message: "must set exactly one of ref or inline",
		}}
	}
	return nil
}

func isValidSemver(v string) bool {
	candidate := v
	if !strings.HasPrefix(candidate, "v") {
		candidate = "v" + candidate
	}
	return semver.IsValid(candidate)
}
