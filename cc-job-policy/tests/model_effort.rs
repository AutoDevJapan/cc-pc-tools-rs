use cc_job_policy::model_profile;

#[test]
fn unattended_default_keeps_sol_max_for_implementation_and_readers() {
    for (name, kind) in [
        ("req-3081", "implementation"),
        ("research-3081", "research"),
        ("check-3081", "verification"),
        ("rev-3081", "review"),
    ] {
        let profile = model_profile(name, kind);
        assert_eq!(profile.model, "gpt-6.1-sol", "{name}/{kind}");
        assert_eq!(profile.effort, "max", "{name}/{kind}");
    }
}
