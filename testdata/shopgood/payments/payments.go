package payments

type Request struct {
	Amount int
	OrderID string
}

type Handler interface {
	Handle(req Request) error
}

type ChargeHandler struct{}

func (ChargeHandler) Handle(req Request) error {
	if req.OrderID == "" {
		return err("missing order")
	}
	if req.Amount <= 0 {
		return err("invalid amount")
	}
	return ProcessPayment(req.OrderID, req.Amount)
}

type RefundHandler struct{}

func (RefundHandler) Handle(req Request) error {
	if req.OrderID == "" {
		return err("missing order")
	}
	if req.Amount <= 0 {
		return err("invalid amount")
	}
	return ProcessPayment(req.OrderID, -req.Amount)
}

func ProcessPayment(orderID string, amount int) error {
	if orderID == "" {
		return err("empty")
	}
	_ = amount
	return nil
}

type payErr string

func (e payErr) Error() string { return string(e) }
func err(msg string) error     { return payErr(msg) }
