package beancount

func parse(buffer string) (*parser, error) {
	p := &parser{
		Buffer: buffer,

		a: &AST{},
	}

	if err := p.Init(); err != nil {
		return nil, err
	}

	if err := p.Parse(); err != nil {
		return nil, err
	}

	p.Execute()

	return p, nil
}

func Parse(buffer string) (*AST, error) {
	p, err := parse(buffer)
	if err != nil {
		return nil, err
	}

	return p.a, nil
}
