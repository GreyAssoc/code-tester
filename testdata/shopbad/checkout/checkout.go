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
	_ = o // BAD: discarded — never reaches ProcessPayment
	return payments.ProcessPayment("x", amount)
}

func QuickCheckout(id string, amount int) error {
	o, err := orders.ValidateOrder(id, amount)
	if err != nil {
		return err
	}
	if o == nil { // BAD: only compared
		return err
	}
	return payments.ProcessPayment("x", amount)
}
