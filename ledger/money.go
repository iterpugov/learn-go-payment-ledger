package ledger

import "errors"

// Money — сумма в минимальных единицах валюты (копейки/центы).
// Не использовать float для денег.
type Money int64

func NewMoney(minorUnits int64) (Money, error) {
	if minorUnits <= 0 {
		return 0, errors.New("minor units must be positive")
	}
	return Money(minorUnits), nil
}

func (m Money) Int64() int64 {
	return int64(m)
}

func (m Money) IsPositive() bool {
	return m > 0
}
