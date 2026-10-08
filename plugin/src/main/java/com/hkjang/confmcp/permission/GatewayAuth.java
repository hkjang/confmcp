package com.hkjang.confmcp.permission;

import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import javax.inject.Inject;
import javax.inject.Named;
import javax.servlet.http.HttpServletRequest;
import javax.ws.rs.core.Response;

/**
 * Gate in front of every gateway endpoint.
 *
 * The REST resources are anonymous at the Atlassian layer (the gateway holds
 * no Confluence session), so this signature check is the only thing that
 * stands between an arbitrary HTTP client and other users' permission data.
 * It is a singleton so the replay cache is shared by all requests on the node.
 */
@Named("confmcpGatewayAuth")
public class GatewayAuth {

    private static final Logger log = LoggerFactory.getLogger(GatewayAuth.class);

    private final SecretStore secretStore;
    private final SignatureVerifier verifier = new SignatureVerifier();

    @Inject
    public GatewayAuth(SecretStore secretStore) {
        this.secretStore = secretStore;
    }

    /**
     * @return null when the request carries a valid, fresh gateway signature,
     * otherwise the error response to send.
     */
    public Response check(HttpServletRequest request, byte[] body) {
        String secret = secretStore.secret();
        if (secret == null) {
            return Json.error(503, "secret not configured");
        }
        SignatureVerifier.Result result = verifier.verify(secret,
                request.getMethod(),
                request.getRequestURI(),
                body,
                request.getHeader(SignatureVerifier.HEADER_TIMESTAMP),
                request.getHeader(SignatureVerifier.HEADER_NONCE),
                request.getHeader(SignatureVerifier.HEADER_SIGNATURE));
        if (result == SignatureVerifier.Result.OK) {
            return null;
        }
        log.warn("confmcp: rejected {} {} from {}: {}", request.getMethod(), request.getRequestURI(),
                request.getRemoteAddr(), result);
        if (result == SignatureVerifier.Result.REPLAY_CACHE_FULL) {
            return Json.error(503, result.message());
        }
        return Json.error(401, result.message());
    }
}
