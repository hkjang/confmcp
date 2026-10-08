package com.hkjang.confmcp.permission;

import com.atlassian.sal.api.user.UserKey;
import org.codehaus.jackson.JsonNode;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import javax.inject.Inject;
import javax.ws.rs.DELETE;
import javax.ws.rs.GET;
import javax.ws.rs.PUT;
import javax.ws.rs.Path;
import javax.ws.rs.Produces;
import javax.ws.rs.core.MediaType;
import javax.ws.rs.core.Response;
import java.io.InputStream;
import java.util.LinkedHashMap;
import java.util.Map;

/**
 * Shared-secret administration, for logged-in Confluence system administrators
 * only. Deliberately not signature-protected: this is how the first secret
 * gets in. The secret is write-only; nothing ever returns it.
 *
 * <pre>
 * GET    /rest/confmcp/1.0/config   -> {"configured":true|false,"source":"pluginSettings|systemProperty|none"}
 * PUT    /rest/confmcp/1.0/config   {"secret":"..."}
 * DELETE /rest/confmcp/1.0/config   removes the stored secret (the system property, if any, still applies)
 * </pre>
 */
@Path("/config")
@Produces({MediaType.APPLICATION_JSON})
public class ConfigResource {

    private static final Logger log = LoggerFactory.getLogger(ConfigResource.class);

    private final SecretStore secretStore;
    private final PluginEnvironment env;

    @Inject
    public ConfigResource(SecretStore secretStore, PluginEnvironment env) {
        this.secretStore = secretStore;
        this.env = env;
    }

    @GET
    public Response get() {
        Response denied = requireSystemAdmin();
        if (denied != null) {
            return denied;
        }
        return Json.ok(state());
    }

    @PUT
    public Response put(InputStream bodyStream) {
        Response denied = requireSystemAdmin();
        if (denied != null) {
            return denied;
        }
        String secret;
        try {
            JsonNode in = Json.parse(Json.readBody(bodyStream));
            secret = Json.text(in, "secret");
        } catch (Json.BadRequest e) {
            return Json.error(e);
        }
        secret = secret == null ? "" : secret.trim();
        if (secret.length() < SecretStore.MIN_LENGTH || secret.length() > SecretStore.MAX_LENGTH) {
            return Json.error(400, "secret must be " + SecretStore.MIN_LENGTH + "-" + SecretStore.MAX_LENGTH + " characters");
        }
        for (int i = 0; i < secret.length(); i++) {
            if (Character.isISOControl(secret.charAt(i))) {
                return Json.error(400, "secret must not contain control characters");
            }
        }
        try {
            secretStore.store(secret);
        } catch (RuntimeException e) {
            log.error("confmcp: storing the shared secret failed: {}", e.toString());
            return Json.error(500, "could not store secret");
        }
        log.warn("confmcp: shared secret updated by {}", env.remoteUserKey());
        return Json.ok(state());
    }

    @DELETE
    public Response delete() {
        Response denied = requireSystemAdmin();
        if (denied != null) {
            return denied;
        }
        try {
            secretStore.store(null);
        } catch (RuntimeException e) {
            log.error("confmcp: removing the shared secret failed: {}", e.toString());
            return Json.error(500, "could not remove secret");
        }
        log.warn("confmcp: stored shared secret removed by {}", env.remoteUserKey());
        return Json.ok(state());
    }

    private Map<String, Object> state() {
        SecretStore.Source source = secretStore.source();
        Map<String, Object> out = new LinkedHashMap<String, Object>();
        out.put("configured", source != SecretStore.Source.NONE);
        out.put("source", source == SecretStore.Source.PLUGIN_SETTINGS ? "pluginSettings"
                : source == SecretStore.Source.SYSTEM_PROPERTY ? "systemProperty" : "none");
        return out;
    }

    private Response requireSystemAdmin() {
        UserKey key = env.remoteUserKey();
        if (key == null) {
            return Json.error(401, "login required");
        }
        if (!env.isSystemAdmin(key)) {
            return Json.error(403, "system administrator permission required");
        }
        return null;
    }
}
