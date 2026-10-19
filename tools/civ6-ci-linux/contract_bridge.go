package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	contract "cc/civ6-ci-contract"
)

// The only file that imports the parent's module. The Plan crosses as strict
// JSON, so a field drift between the two modules fails closed.
type bridge struct{}

func init() { activeContract = bridge{} }

func convert(p Plan) (contract.Plan, error) {
	var c contract.Plan
	b, e := json.Marshal(p)
	if e != nil {
		return c, e
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.DisallowUnknownFields()
	if d.Decode(&c) != nil {
		return c, errors.New("plan shape differs from the parent contract")
	}
	return c, nil
}

func (bridge) Validate(p Plan) error {
	c, e := convert(p)
	if e != nil {
		return e
	}
	return c.Validate()
}
func (bridge) Labels(p Plan) []string {
	c, e := convert(p)
	if e != nil {
		return nil
	}
	return contract.Labels(c)
}
func (bridge) Verify(ctx context.Context, p Plan, read ReadAPI) error {
	c, e := convert(p)
	if e != nil {
		return e
	}
	return contract.Verify(ctx, c, contract.ReadAPI(read))
}
func (bridge) VerifyAssigned(ctx context.Context, p Plan, runner string, read ReadAPI) error {
	c, e := convert(p)
	if e != nil {
		return e
	}
	return contract.VerifyAssigned(ctx, c, runner, contract.ReadAPI(read))
}
func (bridge) RequireSuccess(ctx context.Context, p Plan, runner string, read ReadAPI) error {
	c, e := convert(p)
	if e != nil {
		return e
	}
	return contract.RequireSuccess(ctx, c, runner, contract.ReadAPI(read))
}
func (bridge) CheckEvent(p Plan, env map[string]string, event []byte) error {
	c, e := convert(p)
	if e != nil {
		return e
	}
	return contract.CheckEvent(c, env, event)
}
