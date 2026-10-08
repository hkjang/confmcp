// Forward 127.0.0.1:<port> inside the Playwright container to the same port on
// the Docker host, so the browser sees the console and Keycloak at the exact
// origins they are configured with (redirect URIs, silent SSO iframe).
//
//   node hack/portfwd.mjs 18088 18280
import net from 'node:net'

const target = process.env.FORWARD_HOST || 'host.docker.internal'
for (const port of process.argv.slice(2).map(Number)) {
  net
    .createServer((client) => {
      const upstream = net.connect(port, target)
      client.pipe(upstream).pipe(client)
      const close = () => { client.destroy(); upstream.destroy() }
      client.on('error', close)
      upstream.on('error', close)
    })
    .listen(port, '127.0.0.1')
}
