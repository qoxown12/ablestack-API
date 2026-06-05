package glueservice

import (
	"fmt"
	"strings"
)

func namesFromList(value any) ([]string, error) {
	items, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("expected list output")
	}
	names := make([]string, 0, len(items))
	for _, item := range items {
		switch v := item.(type) {
		case string:
			if strings.TrimSpace(v) != "" {
				names = append(names, strings.TrimSpace(v))
			}
		case map[string]any:
			name, _ := v["name"].(string)
			if strings.TrimSpace(name) != "" {
				names = append(names, strings.TrimSpace(name))
			}
		}
	}
	return names, nil
}
