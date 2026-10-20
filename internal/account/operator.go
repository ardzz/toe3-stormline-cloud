package account

import "context"

// Operator implements the server operator's account commands (the CLI); it ignores the registration mode.
type Operator struct {
	users  UserAdmin
	hasher PasswordHasher
}

// NewOperator wires an Operator.
func NewOperator(users UserAdmin, hasher PasswordHasher) *Operator {
	return &Operator{users: users, hasher: hasher}
}

// Add creates an account.
func (o *Operator) Add(ctx context.Context, username, password string) (int64, error) {
	id, _, err := createUser(ctx, o.users, o.hasher, username, password)
	return id, err
}

// SetPassword replaces an account's password; the store revokes all of its sessions atomically.
func (o *Operator) SetPassword(ctx context.Context, username, password string) error {
	u, err := NormalizeUsername(username)
	if err != nil {
		return err
	}
	if err := ValidatePassword(password); err != nil {
		return err
	}
	h, err := o.hasher.Hash(ctx, password)
	if err != nil {
		return err
	}
	_, err = o.users.SetPassword(ctx, u, h)
	return err
}

// SetDisabled disables or re-enables an account; disabling revokes all of its sessions atomically in the store.
func (o *Operator) SetDisabled(ctx context.Context, username string, disabled bool) error {
	u, err := NormalizeUsername(username)
	if err != nil {
		return err
	}
	_, err = o.users.SetDisabled(ctx, u, disabled)
	return err
}

// Delete removes an account; the store cascades its sessions.
func (o *Operator) Delete(ctx context.Context, username string) error {
	u, err := NormalizeUsername(username)
	if err != nil {
		return err
	}
	_, err = o.users.Delete(ctx, u)
	return err
}
