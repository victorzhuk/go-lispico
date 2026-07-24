package core

import "fmt"

// Binding is the canonical representation of one local binding form.
type Binding struct {
	Name  Symbol
	Value Value
}

func bindingSyntaxError(form string) string {
	return fmt.Sprintf("%s bindings must be a vector [name value ...] or a list of (name value) pairs", form)
}

// NormalizeBindings accepts vector [name value ...] bindings and list
// ((name value) ...) bindings.
func NormalizeBindings(form string, raw Value) ([]Binding, error) {
	switch rawBindings := raw.(type) {
	case Vector:
		if len(rawBindings.Items)%2 != 0 {
			return nil, fmt.Errorf("%s; vector form must have even number of elements", bindingSyntaxError(form))
		}
		b := make([]Binding, 0, len(rawBindings.Items)/2)
		for i := 0; i < len(rawBindings.Items); i += 2 {
			name, ok := rawBindings.Items[i].(Symbol)
			if !ok {
				return nil, fmt.Errorf("%s; binding name must be a symbol, got %T", bindingSyntaxError(form), rawBindings.Items[i])
			}
			b = append(b, Binding{Name: name, Value: rawBindings.Items[i+1]})
		}
		return b, nil
	case List:
		if len(rawBindings.Items) == 0 {
			return nil, nil
		}
		b := make([]Binding, 0, len(rawBindings.Items))
		for _, pair := range rawBindings.Items {
			bindingPair, ok := pair.(List)
			if !ok {
				return nil, fmt.Errorf("%s; each binding must be a two-item list", bindingSyntaxError(form))
			}
			if len(bindingPair.Items) != 2 {
				return nil, fmt.Errorf("%s; each binding pair must have 2 items", bindingSyntaxError(form))
			}
			name, ok := bindingPair.Items[0].(Symbol)
			if !ok {
				return nil, fmt.Errorf("%s; binding name must be a symbol, got %T", bindingSyntaxError(form), bindingPair.Items[0])
			}
			b = append(b, Binding{Name: name, Value: bindingPair.Items[1]})
		}
		return b, nil
	default:
		return nil, fmt.Errorf("%s", bindingSyntaxError(form))
	}
}
