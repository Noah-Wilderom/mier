import './JiraIntegration.css'

// Jira is planned; an enabled installation action would imply a real provider.
export function JiraIntegration() {
  return <article className="jira-integration" aria-label="Jira app integration">
    <img className="jira-integration-logo" src="/integrations/jira.svg" alt="Jira" width={36} height={36}/>
    <div className="jira-integration-details">
      <div className="jira-integration-heading"><h3>Jira</h3><span className="jira-integration-preview">Coming soon</span></div>
      <p className="jira-integration-description">Link issues to your review workflow. Jira account connection is planned.</p>
    </div>
    <div className="jira-install-action"><button type="button" className="btn jira-install-button is-disabled" disabled>Coming soon</button></div>
  </article>
}
