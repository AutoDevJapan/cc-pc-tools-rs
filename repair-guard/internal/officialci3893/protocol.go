package officialci3893

import "context"

// Call deliberately has no root, hash, URL, repo, task, job, or receipt JSON.
// The executable's eventual trusted wiring supplies those independent proofs.
type Call struct{ operation, receipt string }

func ParseCall(args []string) (Call, error) {
	if len(args) == 1 && args[0] == "--construct-official-ci3893" {
		return Call{operation: "construct"}, nil
	}
	if len(args) == 2 && hexN(args[1], 32) {
		switch args[0] {
		case "--resume-official-ci3893":
			return Call{"resume", args[1]}, nil
		case "--consume-official-ci3893":
			return Call{"consume", args[1]}, nil
		}
	}
	return Call{}, ErrDenied
}

type Result struct {
	PurposeVerified           bool   `json:"purpose_verified"`
	ReceiptID                 string `json:"receipt_id,omitempty"`
	State                     State  `json:"state,omitempty"`
	OriginalRegistryRecovered bool   `json:"original_registry_recovered"`
	GeneralAccessGranted      bool   `json:"access_allowed"`
	LaunchPerformed           bool   `json:"launch_performed"`
}

// Execute is only a typed proposal for CLI wiring. It is not called by the live
// guard. Failures preserve the receipt ID/state where available for supervision.
func Execute(ctx context.Context, e *Engine, c Call, use func([]Payload) error) (Result, error) {
	var out Result
	if e == nil {
		return out, ErrDenied
	}
	switch c.operation {
	case "construct":
		r, err := e.Construct(ctx)
		out.ReceiptID = r.ID
		out.State = r.State
		out.PurposeVerified = err == nil
		return out, err
	case "resume":
		if err := e.Resume(ctx, c.receipt); err != nil {
			return out, err
		}
		out.ReceiptID = c.receipt
		out.State = Complete
		out.PurposeVerified = true
		return out, nil
	case "consume":
		if err := e.Consume(ctx, c.receipt, use); err != nil {
			return out, err
		}
		out.ReceiptID = c.receipt
		out.State = Consumed
		out.PurposeVerified = true
		return out, nil
	default:
		return out, ErrDenied
	}
}
