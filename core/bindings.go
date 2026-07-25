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
		items := rawBindings.slice()
		if len(items)%2 != 0 {
			return nil, fmt.Errorf("%s; vector form must have even number of elements", bindingSyntaxError(form))
		}
		b := make([]Binding, 0, len(items)/2)
		for i := 0; i < len(items); i += 2 {
			name, ok := items[i].(Symbol)
			if !ok {
				return nil, fmt.Errorf("%s; binding name must be a symbol, got %T", bindingSyntaxError(form), items[i])
			}
			b = append(b, Binding{Name: name, Value: items[i+1]})
		}
		return b, nil
	case List:
		if rawBindings.Len() == 0 {
			return nil, nil
		}
		items := rawBindings.slice()
		b := make([]Binding, 0, len(items))
		for _, pair := range items {
			bindingPair, ok := pair.(List)
			if !ok {
				return nil, fmt.Errorf("%s; each binding must be a two-item list", bindingSyntaxError(form))
			}
			pairItems := bindingPair.slice()
			if len(pairItems) != 2 {
				return nil, fmt.Errorf("%s; each binding pair must have 2 items", bindingSyntaxError(form))
			}
			name, ok := pairItems[0].(Symbol)
			if !ok {
				return nil, fmt.Errorf("%s; binding name must be a symbol, got %T", bindingSyntaxError(form), pairItems[0])
			}
			b = append(b, Binding{Name: name, Value: pairItems[1]})
		}
		return b, nil
	default:
		return nil, fmt.Errorf("%s", bindingSyntaxError(form))
	}
}
