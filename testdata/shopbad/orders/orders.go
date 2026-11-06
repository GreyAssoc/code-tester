package orders

type Order struct {
	ID     string
	Amount int
}

func ValidateOrder(id string, amount int) (*Order, error) {
	if id == "" {
		return nil, errInvalid("missing id")
	}
	if amount <= 0 {
		return nil, errInvalid("invalid amount")
	}
	return &Order{ID: id, Amount: amount}, nil
}

type invalidError string

func (e invalidError) Error() string { return string(e) }

func errInvalid(msg string) error { return invalidError(msg) }
