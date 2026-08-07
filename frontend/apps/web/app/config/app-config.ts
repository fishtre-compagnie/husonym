export interface SystemAppConfig {
  isAuthEnabled: boolean;
  publicAppBaseUrl: string;
  isHusonymCloud: boolean;
  isStripeEnabled: boolean;
  enableRunLogs: boolean;
  signInProviderId?: string;
  isMetricsServiceEnabled: boolean;
  isJobHooksEnabled: boolean;
  isAccountHooksEnabled: boolean;
  isSlackAccountHookEnabled: boolean;

  upgradeLink: string;
  isGcpCloudStorageConnectionsEnabled: boolean;
  // server-side base url
  husonymApiBaseUrl: string;
  // public (client-side) base url;
  publicHusonymApiBaseUrl: string;
  isRbacEnabled: boolean;
  isPiiDetectionJobEnabled: boolean;
}
