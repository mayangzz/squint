#!/bin/sh
# Builds the throwaway repo the demo is recorded in, plus a `claude` shim that ignores
# your user-level CLAUDE.md / hooks / statusline so none of it ends up in the GIF.
set -e
rm -rf /tmp/shop && mkdir -p /tmp/shop/internal/cart /tmp/shop/internal/payment /tmp/shop/internal/order /tmp/shop-bin
cd /tmp/shop
printf 'module example.com/shop\n\ngo 1.22\n' > go.mod
cat > internal/cart/cart.go <<'GO'
package cart

type Item struct {
	SKU   string
	Qty   int
	Price int64 // cents
}

type Cart struct{ Items []Item }

// Total returns the cart total in cents.
func (c *Cart) Total() int64 {
	var sum int64
	for _, it := range c.Items {
		sum += it.Price * int64(it.Qty)
	}
	// TODO: apply coupon discounts before returning
	return sum
}

// TODO: merge duplicate SKUs when adding items
func (c *Cart) Add(it Item) { c.Items = append(c.Items, it) }
GO
cat > internal/payment/charge.go <<'GO'
package payment

import "errors"

// Charge sends the amount to the payment gateway.
func Charge(userID string, cents int64) error {
	if cents <= 0 {
		return errors.New("invalid amount")
	}
	// TODO: retry on gateway timeout, currently fails on first error
	return gateway(userID, cents)
}

func gateway(userID string, cents int64) error { return nil }
GO
cat > internal/order/order.go <<'GO'
package order

import (
	"example.com/shop/internal/cart"
	"example.com/shop/internal/payment"
)

// Checkout charges the user and creates an order.
func Checkout(userID string, c *cart.Cart) error {
	// TODO: wrap charge + insert in one transaction
	if err := payment.Charge(userID, c.Total()); err != nil {
		return err
	}
	return save(userID, c)
}

func save(userID string, c *cart.Cart) error { return nil }
GO
git init -q && git config user.name "Demo Dev" && git config user.email dev@example.com
git add -A && git commit -qm init --no-verify

REAL=$(command -v claude)
cat > /tmp/shop-bin/claude <<SH
#!/bin/sh
exec "$REAL" --setting-sources project --allowedTools Bash,Read,Grep,Glob "\$@"
SH
chmod +x /tmp/shop-bin/claude
echo "ready: /tmp/shop (trust the folder once by running 'claude' there before recording)"
