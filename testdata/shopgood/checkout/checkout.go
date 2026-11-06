package checkout

import (
	"example.com/shop/orders"
	"example.com/shop/payments"
)

func Checkout(id string, amount int) error {
	o, err := orders.ValidateOrder(id, amount)
	if err != nil {
		return err
	}
	o = withDiscount(o)
	return payments.ProcessPayment(o.ID, o.Amount)
}

func QuickCheckout(id string, amount int) error {
	o, err := orders.ValidateOrder(id, amount)
	if err != nil {
		return err
	}
	return payments.ProcessPayment(o.ID, o.Amount)
}

func withDiscount(o *orders.Order) *orders.Order {
	if o.Amount > 100 {
		o.Amount -= 10
	}
	return o
}
