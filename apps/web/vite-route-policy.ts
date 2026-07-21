type DevelopmentProxyOptions = {
  target: string
  changeOrigin: boolean
}

const developmentProxyPaths = ['/api', '/assets', '/healthz', '/readyz']

export function createDevelopmentProxy(
  target: string,
): Record<string, DevelopmentProxyOptions> {
  return Object.fromEntries(
    developmentProxyPaths.map((path) => [path, { target, changeOrigin: true }]),
  )
}

export function createNavigationFallbackDenylist(): RegExp[] {
  return [
    /^\/api\//,
    /^\/assets\//,
    /^\/healthz$/,
    /^\/readyz(?:\?.*)?$/,
  ]
}
