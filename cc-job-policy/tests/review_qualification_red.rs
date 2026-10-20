#[test]
fn automatic_review_default_differs_from_the_normal_sol_writer() {
    let writer = cc_job_policy::model_profile("todo-3081", "implementation");
    let review = cc_job_policy::model_profile("rev-3081", "review");
    assert_eq!(writer.model, "gpt-6.1-sol");
    assert_eq!(review.model, "anthropic/claude-opus-5-5");
    assert_ne!(writer.model, review.model);
    assert_eq!(review.effort, "max");
}