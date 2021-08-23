package keygen

import (
	"fmt"

	"github.com/taurusgroup/multi-party-sig/internal/hash"
	"github.com/taurusgroup/multi-party-sig/internal/round"
	"github.com/taurusgroup/multi-party-sig/internal/types"
	"github.com/taurusgroup/multi-party-sig/pkg/math/curve"
	"github.com/taurusgroup/multi-party-sig/pkg/math/polynomial"
	"github.com/taurusgroup/multi-party-sig/pkg/party"
	sch "github.com/taurusgroup/multi-party-sig/pkg/zk/sch"
)

// This round corresponds with steps 5 of Round 1, 1 of Round 2, Figure 1 in the Frost paper:
//   https://eprint.iacr.org/2020/852.pdf
type round2 struct {
	*round1
	// f_i is the polynomial this participant uses to share their contribution to
	// the secret
	f_i *polynomial.Polynomial
	// Phi contains the polynomial commitment for each participant, ourselves included.
	//
	// Phi[l][k] corresponds to ϕₗₖ in the Frost paper.
	Phi map[party.ID]*polynomial.Exponent
	// ChainKeyDecommitment will be used to decommit our contribution to the chain key
	ChainKeyDecommitment hash.Decommitment

	// ChainKey will be the final bit of randomness everybody contributes to.
	//
	// This is an addition to FROST, which we include for key derivation
	ChainKeys map[party.ID]types.RID
	// ChainKeyCommitments holds the commitments for the chain key contributions
	ChainKeyCommitments map[party.ID]hash.Commitment
}

type message2 struct {
	// Phi_i is the commitment to the polynomial that this participant generated.
	Phi_i *polynomial.Exponent
	// Sigma_i is the Schnorr proof of knowledge of the participant's secret
	Sigma_i *sch.Proof
	// Commitment = H(cᵢ, uᵢ)
	Commitment hash.Commitment
}

// VerifyMessage implements round.Round.
func (r *round2) VerifyMessage(msg round.Message) error {
	from := msg.From
	body, ok := msg.Content.(*message2)
	if !ok || body == nil {
		return round.ErrInvalidContent
	}

	// check nil
	if body.Sigma_i == nil || body.Phi_i == nil {
		return round.ErrNilFields
	}

	// These steps come from Figure 1, Round 1 of the Frost paper

	// 5. "Upon receiving ϕₗ, σₗ from participants 1 ⩽ l ⩽ n, participant
	// Pᵢ verifies σₗ = (Rₗ, μₗ), aborting on failure, by checking
	// Rₗ = μₗ * G - cₗ * ϕₗ₀, where cₗ = H(l, ctx, ϕₗ₀, Rₗ).
	//
	// Upon success, participants delete { σₗ | 1 ⩽ l ⩽ n }"
	//
	// Note: I've renamed Cₗ to Φₗ, as in the previous round.
	// R_l = Rₗ, mu_l = μₗ
	//
	// To see why this is correct, compare this verification with the proof we
	// produced in the previous round. Note how we do the same hash cloning,
	// but this time with the ID of the message sender.
	if !body.Sigma_i.Verify(r.Helper.HashForID(from), body.Phi_i.Constant()) {
		return fmt.Errorf("failed to verify Schnorr proof for party %s", from)
	}

	return nil
}

// StoreMessage implements round.Round.
func (r *round2) StoreMessage(msg round.Message) error {
	from, body := msg.From, msg.Content.(*message2)
	r.Phi[from] = body.Phi_i
	r.ChainKeyCommitments[from] = body.Commitment
	return nil
}

func (r *round2) Finalize(out chan<- *round.Message) (round.Round, error) {
	// These steps come from Figure 1, Round 2 of the Frost paper

	// 1. "Each P_i securely sends to each other participant Pₗ a secret share
	// (l, fᵢ(l)), deleting f_i and each share afterward except for (i, fᵢ(i)),
	// which they keep for themselves."

	for _, l := range r.OtherPartyIDs() {
		err := r.SendMessage(out, &message3{
			F_li:         r.f_i.Evaluate(l.Scalar(r.Group())),
			C_l:          r.ChainKeys[r.SelfID()],
			Decommitment: r.ChainKeyDecommitment,
		}, l)
		if err != nil {
			return r, err
		}
	}

	selfShare := r.f_i.Evaluate(r.SelfID().Scalar(r.Group()))
	return &round3{
		round2:    r,
		shareFrom: map[party.ID]curve.Scalar{r.SelfID(): selfShare},
	}, nil
}

// MessageContent implements round.Round.
func (round2) MessageContent() round.Content { return &message2{} }

// Number implements round.Round.
func (round2) Number() round.Number { return 2 }

// Init implements round.Content.
func (m *message2) Init(group curve.Curve) {
	m.Phi_i = polynomial.EmptyExponent(group)
	m.Sigma_i = sch.EmptyProof(group)
}
