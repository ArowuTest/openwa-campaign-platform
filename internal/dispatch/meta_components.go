package dispatch

import (
	"errors"
	"strings"
)

func renderMetaRequestComponents(request GatewayRequest) ([]any, error) {
	if len(request.MetaComponents) == 0 {
		return renderLegacyMetaRequestComponents(request)
	}
	components := make([]any, 0, len(request.MetaComponents))
	seenBody, seenHeader := false, false
	for _, component := range request.MetaComponents {
		typeName := strings.ToUpper(strings.TrimSpace(component.Type))
		subType := strings.ToUpper(strings.TrimSpace(component.SubType))
		switch typeName {
		case "BODY":
			if seenBody || component.Index != 0 || (subType != "" && subType != "TEXT") {
				return nil, errors.New("invalid Meta body component")
			}
			seenBody = true
			parameters := make([]any, 0, len(component.Parameters))
			for _, parameter := range component.Parameters {
				if strings.ToUpper(strings.TrimSpace(parameter.Type)) != "TEXT" || strings.TrimSpace(parameter.Text) == "" || strings.TrimSpace(parameter.Link) != "" {
					return nil, errors.New("invalid Meta body parameter")
				}
				parameters = append(parameters, map[string]any{"type": "text", "text": parameter.Text})
			}
			components = append(components, map[string]any{"type": "body", "parameters": parameters})
		case "HEADER":
			if seenHeader || component.Index != 0 || len(component.Parameters) != 1 {
				return nil, errors.New("invalid Meta header component")
			}
			seenHeader = true
			expected := strings.ToUpper(strings.TrimSpace(request.MessageType))
			if expected == "TEXT" || subType != expected {
				return nil, errors.New("Meta header component does not match message type")
			}
			parameter := component.Parameters[0]
			if strings.ToUpper(strings.TrimSpace(parameter.Type)) != subType || strings.TrimSpace(parameter.Link) == "" || strings.TrimSpace(parameter.Text) != "" {
				return nil, errors.New("invalid Meta media header parameter")
			}
			lower := strings.ToLower(subType)
			components = append(components, map[string]any{"type": "header", "parameters": []any{map[string]any{"type": lower, lower: map[string]any{"link": strings.TrimSpace(parameter.Link)}}}})
		default:
			return nil, errors.New("unsupported Meta template component")
		}
	}
	if request.MessageType != "text" && !seenHeader {
		return nil, errors.New("media Meta template requires a typed header component")
	}
	return components, nil
}

func renderLegacyMetaRequestComponents(request GatewayRequest) ([]any, error) {
	components := make([]any, 0, 2)
	if len(request.MetaBodyParameters) > 0 {
		parameters := make([]any, 0, len(request.MetaBodyParameters))
		for _, value := range request.MetaBodyParameters {
			value = strings.TrimSpace(value)
			if value == "" {
				return nil, errors.New("Meta body parameter is empty")
			}
			parameters = append(parameters, map[string]any{"type": "text", "text": value})
		}
		components = append(components, map[string]any{"type": "body", "parameters": parameters})
	}
	if request.MessageType != "text" {
		link := strings.TrimSpace(request.MediaURL)
		if link == "" {
			return nil, errors.New("media Meta template requires media URL")
		}
		components = append(components, map[string]any{"type": "header", "parameters": []any{map[string]any{"type": request.MessageType, request.MessageType: map[string]any{"link": link}}}})
	}
	return components, nil
}
