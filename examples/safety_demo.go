package main

import (
	"fmt"
	"strconv"
)

type Account struct {
	Name string
	Balance int64
}

func add(a int64, b int64) int64 {
	if !((a >= 0)) { panic("requires violated: a >= 0") }
	if !((b >= 0)) { panic("requires violated: b >= 0") }
	return (a + b)
}

func show_account(acc Account) {
	fmt.Println(("Account: " + acc.Name))
	fmt.Println(("Balance: " + strconv.FormatInt(int64(acc.Balance), 10)))
	return
}

func deposit(acc Account, amount int64) (Account, error) {
	if !((amount > 0)) { panic("requires violated: amount > 0") }
	if (amount <= 0) {
		return Account{}, fmt.Errorf("%s", ("invalid deposit amount: " + strconv.FormatInt(int64(amount), 10)))
	}
	new_balance := add(acc.Balance, amount)
	return Account{Name: acc.Name, Balance: new_balance}, nil
}

func withdraw(acc Account, amount int64) (Account, error) {
	if !((amount > 0)) { panic("requires violated: amount > 0") }
	if (amount > acc.Balance) {
		return Account{}, fmt.Errorf("%s", ("insufficient funds, have: " + strconv.FormatInt(int64(acc.Balance), 10)))
	}
	return Account{Name: acc.Name, Balance: (acc.Balance - amount)}, nil
}

func transfer(from Account, to Account, amount int64) (string, error) {
	_t1, _t2 := withdraw(from, amount)
	if _t2 != nil {
		e := _t2.Error()
		return "", fmt.Errorf("%s", ("withdraw failed: " + e))
	}
	from2 := _t1
	_t3, _t4 := deposit(to, amount)
	if _t4 != nil {
		e := _t4.Error()
		return "", fmt.Errorf("%s", ("deposit failed: " + e))
	}
	to2 := _t3
	return ((from2.Name + " -> ") + to2.Name), nil
}

func main() {
	alice := Account{Name: "Alice", Balance: 1000}
	bob := Account{Name: "Bob", Balance: 250}
	show_account(alice)
	show_account(bob)
	_t1, _t2 := transfer(alice, bob, 400)
	if _t2 != nil {
		e := _t2.Error()
		fmt.Println(("Transfer failed: " + e))
	} else {
		msg := _t1
		fmt.Println(("Transfer ok: " + msg))
	}
	_t3, _t4 := transfer(alice, bob, 5000)
	if _t4 != nil {
		e := _t4.Error()
		fmt.Println(("Transfer failed: " + e))
	} else {
		msg := _t3
		fmt.Println(("Transfer ok: " + msg))
	}
	return
}

