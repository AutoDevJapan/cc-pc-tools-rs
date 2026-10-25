//! Prepared rule for one existing, unstarted review. No runtime adapter or mutation.
//!
//! The normal scheduler must verify and recheck the supplied registration proof,
//! current row, OAuth inventory and authors. A caller-created value is not authority.

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Origin {
    Absent,
    Null,
    Policy,
    OwnerRequest,
    Unknown,
}

#[derive(Clone, Copy)]
pub struct Registration<'a> {
    pub source_job: &'a str,
    pub thread: &'a str,
    pub turn: &'a str,
    pub item: &'a str,
    pub command_sha256: &'a str,
    pub output_sha256: &'a str,
    pub normal_api_bound: bool,
    pub completed_successfully: bool,
    pub added_3649: bool,
    pub omitted_origin_flags: bool,
    pub current_owner_scope_verified: bool,
}

impl Registration<'_> {
    fn bound(&self) -> bool {
        self.source_job == "20261024-201241-todo-3387"
            && self.thread == "01a169de-626d-7270-9de7-362e2cc8af25"
            && self.turn == "01a16d9d-54fe-77c0-9ce3-1a46594978fd"
            && self.item == "exec-27beecc5-8f7a-47c3-927f-9f0a7208a390"
            && self.command_sha256
                == "31ad65c65a2f203326add7cd2b1feb438034671fa12255603abdd40312976deb"
            && self.output_sha256
                == "9eec3fe88e851eaaf196a27033696cee8a5e34bbae59d222c7fa3778c0a0d992"
            && self.normal_api_bound
            && self.completed_successfully
            && self.added_3649
            && self.omitted_origin_flags
            && self.current_owner_scope_verified
    }
}

#[derive(Clone, Copy)]
pub struct Pending<'a> {
    pub todo_id: u64,
    pub project: &'a str,
    pub todo: bool,
    pub dispatchable: bool,
    pub jobs_empty: bool,
    pub live_worker_absent: bool,
    pub stops_absent: bool,
    pub dependencies_ready: bool,
    pub other_waits_absent: bool,
    pub reservation_unchanged: bool,
    pub source_head: &'a str,
    pub spec_sha256: &'a str,
    pub requested_model: &'a str,
    pub requested_effort: &'a str,
    pub model_origin: Origin,
    pub effort_origin: Origin,
    pub quota_exhausted_recorded: bool,
    pub quota_until: f64,
    pub not_before: f64,
    pub state_sha256: [u8; 32],
}

#[derive(Clone, Copy)]
pub struct OAuthInventory {
    pub observed_at: f64,
    pub anthropic_verified_exhausted: bool,
    pub openai_verified_available: bool,
    pub subscription_only_no_additional_spend: bool,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Reason {
    Scope,
    NotUnstarted,
    StopOrDependency,
    SourceOrReservation,
    OriginUnverified,
    RegistrationUnverified,
    NotOnlyQuotaWait,
    OAuthUnverified,
    AuthorModelUnverified,
    SameAuthorModel,
    Changed,
}

#[derive(Debug, PartialEq)]
pub struct Plan {
    pub todo_id: u64,
    pub selected_model: &'static str,
    pub selected_effort: &'static str,
    pub expected_state_sha256: [u8; 32],
    pub prior_quota_deadline: f64,
}

/// Produce a plan only. Origins, deadlines, reservations and rows remain untouched.
pub fn plan(
    pending: Pending<'_>,
    registration: Registration<'_>,
    inventory: OAuthInventory,
    now: f64,
    author_models: Option<&[&str]>,
) -> Result<Plan, Reason> {
    if pending.todo_id != 3649 || pending.project != "localai-web-rs" {
        return Err(Reason::Scope);
    }
    if !pending.todo || !pending.dispatchable || !pending.jobs_empty || !pending.live_worker_absent
    {
        return Err(Reason::NotUnstarted);
    }
    if !pending.stops_absent || !pending.dependencies_ready || !pending.other_waits_absent {
        return Err(Reason::StopOrDependency);
    }
    if !pending.reservation_unchanged
        || pending.source_head != "173c3a4aa925bb03c8e1c109ea598e05f7d0dc9a"
        || pending.spec_sha256 != "0699a3aa47d93e2cc1014964f4f913bbb6f1d986bb9fb3fa52159453bebd441e"
        || pending.requested_model != "claude-opus-5-5"
        || pending.requested_effort != "max"
    {
        return Err(Reason::SourceOrReservation);
    }
    if pending.model_origin != Origin::Absent || pending.effort_origin != Origin::Absent {
        return Err(Reason::OriginUnverified);
    }
    if !registration.bound() {
        return Err(Reason::RegistrationUnverified);
    }
    if !now.is_finite()
        || !pending.quota_exhausted_recorded
        || !pending.quota_until.is_finite()
        || pending.quota_until <= now
        || pending.quota_until != pending.not_before
    {
        return Err(Reason::NotOnlyQuotaWait);
    }
    if !inventory.observed_at.is_finite()
        || inventory.observed_at > now + 1.
        || now - inventory.observed_at > 60.
        || !inventory.anthropic_verified_exhausted
        || !inventory.openai_verified_available
        || !inventory.subscription_only_no_additional_spend
    {
        return Err(Reason::OAuthUnverified);
    }
    let authors = author_models
        .filter(|models| !models.is_empty())
        .ok_or(Reason::AuthorModelUnverified)?;
    if authors.iter().any(|model| model.is_empty()) {
        return Err(Reason::AuthorModelUnverified);
    }
    if authors
        .iter()
        .any(|model| matches!(*model, "gpt-6-luna" | "openai/gpt-6-luna"))
    {
        return Err(Reason::SameAuthorModel);
    }
    Ok(Plan {
        todo_id: 3649,
        selected_model: "gpt-6-luna",
        selected_effort: "max",
        expected_state_sha256: pending.state_sha256,
        prior_quota_deadline: pending.not_before,
    })
}

impl Plan {
    /// Final CAS prerequisite only; callers must still recheck normal admission.
    pub fn unchanged(&self, current_state_sha256: [u8; 32]) -> Result<(), Reason> {
        if self.expected_state_sha256 == current_state_sha256 {
            Ok(())
        } else {
            Err(Reason::Changed)
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn fixture() -> (Pending<'static>, Registration<'static>, OAuthInventory) {
        (
            Pending {
                todo_id: 3649,
                project: "localai-web-rs",
                todo: true,
                dispatchable: true,
                jobs_empty: true,
                live_worker_absent: true,
                stops_absent: true,
                dependencies_ready: true,
                other_waits_absent: true,
                reservation_unchanged: true,
                source_head: "173c3a4aa925bb03c8e1c109ea598e05f7d0dc9a",
                spec_sha256: "0699a3aa47d93e2cc1014964f4f913bbb6f1d986bb9fb3fa52159453bebd441e",
                requested_model: "claude-opus-5-5",
                requested_effort: "max",
                model_origin: Origin::Absent,
                effort_origin: Origin::Absent,
                quota_exhausted_recorded: true,
                quota_until: 1793030399.891,
                not_before: 1793030399.891,
                state_sha256: [7; 32],
            },
            Registration {
                source_job: "20261024-201241-todo-3387",
                thread: "01a169de-626d-7270-9de7-362e2cc8af25",
                turn: "01a16d9d-54fe-77c0-9ce3-1a46594978fd",
                item: "exec-27beecc5-8f7a-47c3-927f-9f0a7208a390",
                command_sha256: "31ad65c65a2f203326add7cd2b1feb438034671fa12255603abdd40312976deb",
                output_sha256: "9eec3fe88e851eaaf196a27033696cee8a5e34bbae59d222c7fa3778c0a0d992",
                normal_api_bound: true,
                completed_successfully: true,
                added_3649: true,
                omitted_origin_flags: true,
                current_owner_scope_verified: true,
            },
            OAuthInventory {
                observed_at: 1000.,
                anthropic_verified_exhausted: true,
                openai_verified_available: true,
                subscription_only_no_additional_spend: true,
            },
        )
    }

    #[test]
    fn fixed_source_and_fresh_oauth_produce_only_a_bound_plan() {
        let (pending, proof, inventory) = fixture();
        let result = plan(pending, proof, inventory, 1000., Some(&["gpt-6.1-sol"])).unwrap();
        assert_eq!(result.todo_id, 3649);
        assert_eq!(result.selected_model, "gpt-6-luna");
        assert_eq!(result.selected_effort, "max");
        assert_eq!(result.prior_quota_deadline, pending.not_before);
        assert_eq!(pending.model_origin, Origin::Absent);
        assert_eq!(result.unchanged([7; 32]), Ok(()));
        assert_eq!(result.unchanged([8; 32]), Err(Reason::Changed));
    }

    #[test]
    fn recorded_or_unknown_origins_are_never_replaced_by_policy() {
        let (pending, proof, inventory) = fixture();
        for origin in [
            Origin::Null,
            Origin::Policy,
            Origin::OwnerRequest,
            Origin::Unknown,
        ] {
            let mut changed = pending;
            changed.model_origin = origin;
            assert_eq!(
                plan(changed, proof, inventory, 1000., Some(&["gpt-6.1-sol"])),
                Err(Reason::OriginUnverified)
            );
            changed = pending;
            changed.effort_origin = origin;
            assert_eq!(
                plan(changed, proof, inventory, 1000., Some(&["gpt-6.1-sol"])),
                Err(Reason::OriginUnverified)
            );
        }
    }

    #[test]
    fn stops_dependencies_started_jobs_and_other_deadlines_keep_their_holds() {
        let (pending, proof, inventory) = fixture();
        for field in 0..9 {
            let mut changed = pending;
            match field {
                0 => changed.todo = false,
                1 => changed.dispatchable = false,
                2 => changed.jobs_empty = false,
                3 => changed.live_worker_absent = false,
                4 => changed.stops_absent = false,
                5 => changed.dependencies_ready = false,
                6 => changed.other_waits_absent = false,
                7 => changed.not_before += 1.,
                _ => changed.reservation_unchanged = false,
            }
            assert!(
                plan(changed, proof, inventory, 1000., Some(&["gpt-6.1-sol"])).is_err(),
                "{field}"
            );
        }
    }

    #[test]
    fn registration_claims_require_every_original_correspondence() {
        let (pending, proof, inventory) = fixture();
        for field in 0..11 {
            let mut changed = proof;
            match field {
                0 => changed.source_job = "other",
                1 => changed.thread = "other",
                2 => changed.turn = "other",
                3 => changed.item = "other",
                4 => changed.command_sha256 = "other",
                5 => changed.output_sha256 = "other",
                6 => changed.normal_api_bound = false,
                7 => changed.completed_successfully = false,
                8 => changed.added_3649 = false,
                9 => changed.omitted_origin_flags = false,
                _ => changed.current_owner_scope_verified = false,
            }
            assert_eq!(
                plan(pending, changed, inventory, 1000., Some(&["gpt-6.1-sol"])),
                Err(Reason::RegistrationUnverified)
            );
        }
    }

    #[test]
    fn stale_unknown_paid_and_same_author_routes_are_rejected() {
        let (pending, proof, inventory) = fixture();
        for field in 0..7 {
            let mut changed = inventory;
            match field {
                0 => changed.observed_at = 939.99,
                1 => changed.observed_at = 1001.01,
                2 => changed.observed_at = f64::NAN,
                3 => changed.anthropic_verified_exhausted = false,
                4 => changed.openai_verified_available = false,
                5 => changed.subscription_only_no_additional_spend = false,
                _ => changed.observed_at = f64::INFINITY,
            }
            assert_eq!(
                plan(pending, proof, changed, 1000., Some(&["gpt-6.1-sol"])),
                Err(Reason::OAuthUnverified)
            );
        }
        assert_eq!(
            plan(pending, proof, inventory, 1000., None),
            Err(Reason::AuthorModelUnverified)
        );
        assert_eq!(
            plan(pending, proof, inventory, 1000., Some(&[])),
            Err(Reason::AuthorModelUnverified)
        );
        assert_eq!(
            plan(pending, proof, inventory, 1000., Some(&["gpt-6-luna"])),
            Err(Reason::SameAuthorModel)
        );
        assert_eq!(
            plan(
                pending,
                proof,
                inventory,
                1000.,
                Some(&["openai/gpt-6-luna"])
            ),
            Err(Reason::SameAuthorModel)
        );
    }
}
