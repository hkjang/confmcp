package com.hkjang.confmcp.permission;

import com.atlassian.confluence.user.ConfluenceUser;
import com.atlassian.plugins.rest.common.security.AnonymousAllowed;
import com.atlassian.sal.api.transaction.TransactionCallback;
import org.codehaus.jackson.JsonNode;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import javax.inject.Inject;
import javax.servlet.http.HttpServletRequest;
import javax.ws.rs.GET;
import javax.ws.rs.POST;
import javax.ws.rs.Path;
import javax.ws.rs.Produces;
import javax.ws.rs.core.Context;
import javax.ws.rs.core.MediaType;
import javax.ws.rs.core.Response;
import java.io.InputStream;
import java.text.SimpleDateFormat;
import java.util.ArrayList;
import java.util.Date;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.TimeZone;

/**
 * Read-only effective permission lookups for the confmcp gateway.
 *
 * All four endpoints are anonymous at the Atlassian layer and require a valid
 * gateway signature instead (see {@link GatewayAuth}); the subject of every
 * check is the userKey in the body, never the HTTP caller. Nothing here can
 * change a permission.
 *
 * Errors never carry stack traces or exception text: per-operation failures
 * become "unknown" with a reason code, anything else a generic message.
 */
@Path("/")
@Produces({MediaType.APPLICATION_JSON})
public class PermissionResource {

    private static final Logger log = LoggerFactory.getLogger(PermissionResource.class);

    static final int MAX_BATCH = 100;

    private final GatewayAuth gatewayAuth;
    private final PermissionEvaluator evaluator;
    private final UserLookup users;
    private final PluginEnvironment env;

    @Inject
    public PermissionResource(GatewayAuth gatewayAuth, PermissionEvaluator evaluator,
                              UserLookup users, PluginEnvironment env) {
        this.gatewayAuth = gatewayAuth;
        this.evaluator = evaluator;
        this.users = users;
        this.env = env;
    }

    @GET
    @Path("health")
    @AnonymousAllowed
    public Response health(@Context HttpServletRequest request) {
        Response denied = gatewayAuth.check(request, new byte[0]);
        if (denied != null) {
            return denied;
        }
        try {
            Map<String, Object> out = new LinkedHashMap<String, Object>();
            out.put("status", "ok");
            out.put("pluginVersion", env.pluginVersion());
            out.put("confluenceVersion", env.confluenceVersion());
            out.put("buildNumber", env.buildNumber());
            return Json.ok(out);
        } catch (RuntimeException e) {
            return internalError("health", e);
        }
    }

    @POST
    @Path("permissions/check")
    @AnonymousAllowed
    public Response check(@Context HttpServletRequest request, InputStream bodyStream) {
        final JsonNode in;
        try {
            byte[] body = Json.readBody(bodyStream);
            Response denied = gatewayAuth.check(request, body);
            if (denied != null) {
                return denied;
            }
            in = Json.parse(body);
        } catch (Json.BadRequest e) {
            return Json.error(e);
        }
        try {
            return env.inTransaction(new TransactionCallback<Response>() {
                @Override
                public Response doInTransaction() {
                    ConfluenceUser user = users.byKey(Json.text(in, "userKey"));
                    if (user == null) {
                        return Json.error(404, "unknown userKey");
                    }
                    boolean deactivated = users.isDeactivated(user);
                    Map<String, Object> out = new LinkedHashMap<String, Object>();
                    out.put("userKey", user.getKey().getStringValue());
                    out.put("userStatus", deactivated ? "deactivated" : "active");
                    out.put("evaluatedAt", now());
                    out.putAll(evaluator.evaluate(user, deactivated, target(in.get("target")),
                            Json.strings(in, "operations")));
                    logCall("check", in, 1);
                    return Json.ok(out);
                }
            });
        } catch (RuntimeException e) {
            return internalError("check", e);
        }
    }

    @POST
    @Path("permissions/batch")
    @AnonymousAllowed
    public Response batch(@Context HttpServletRequest request, InputStream bodyStream) {
        final JsonNode in;
        try {
            byte[] body = Json.readBody(bodyStream);
            Response denied = gatewayAuth.check(request, body);
            if (denied != null) {
                return denied;
            }
            in = Json.parse(body);
        } catch (Json.BadRequest e) {
            return Json.error(e);
        }
        final JsonNode checks = in.get("checks");
        if (checks == null || !checks.isArray()) {
            return Json.error(400, "checks must be an array");
        }
        if (checks.size() > MAX_BATCH) {
            return Json.error(400, "too many checks (max " + MAX_BATCH + ")");
        }
        try {
            return env.inTransaction(new TransactionCallback<Response>() {
                @Override
                public Response doInTransaction() {
                    ConfluenceUser user = users.byKey(Json.text(in, "userKey"));
                    if (user == null) {
                        return Json.error(404, "unknown userKey");
                    }
                    boolean deactivated = users.isDeactivated(user);
                    List<Object> results = new ArrayList<Object>(checks.size());
                    for (JsonNode check : checks) {
                        // Same order as the request: the gateway pairs answers by position.
                        results.add(evaluator.evaluate(user, deactivated, target(check.get("target")),
                                Json.strings(check, "operations")));
                    }
                    Map<String, Object> out = new LinkedHashMap<String, Object>();
                    out.put("userKey", user.getKey().getStringValue());
                    out.put("userStatus", deactivated ? "deactivated" : "active");
                    out.put("evaluatedAt", now());
                    out.put("results", results);
                    logCall("batch", in, results.size());
                    return Json.ok(out);
                }
            });
        } catch (RuntimeException e) {
            return internalError("batch", e);
        }
    }

    @POST
    @Path("users")
    @AnonymousAllowed
    public Response user(@Context HttpServletRequest request, InputStream bodyStream) {
        final JsonNode in;
        try {
            byte[] body = Json.readBody(bodyStream);
            Response denied = gatewayAuth.check(request, body);
            if (denied != null) {
                return denied;
            }
            in = Json.parse(body);
        } catch (Json.BadRequest e) {
            return Json.error(e);
        }
        final String username = Json.text(in, "username");
        final String userKey = Json.text(in, "userKey");
        if (isBlank(username) && isBlank(userKey)) {
            return Json.error(400, "username or userKey is required");
        }
        try {
            return env.inTransaction(new TransactionCallback<Response>() {
                @Override
                public Response doInTransaction() {
                    ConfluenceUser user = isBlank(userKey) ? null : users.byKey(userKey);
                    if (user == null && !isBlank(username)) {
                        user = users.byName(username);
                    }
                    if (user == null) {
                        return Json.error(404, "user not found");
                    }
                    return Json.ok(users.describe(user));
                }
            });
        } catch (RuntimeException e) {
            return internalError("users", e);
        }
    }

    // ---------------------------------------------------------------- helpers

    private static PermissionEvaluator.Target target(JsonNode node) {
        return new PermissionEvaluator.Target(Json.text(node, "kind"), Json.text(node, "spaceKey"), Json.text(node, "id"));
    }

    private static String now() {
        SimpleDateFormat f = new SimpleDateFormat("yyyy-MM-dd'T'HH:mm:ss'Z'");
        f.setTimeZone(TimeZone.getTimeZone("UTC"));
        return f.format(new Date());
    }

    private static boolean isBlank(String s) {
        return s == null || s.trim().isEmpty();
    }

    private static void logCall(String what, JsonNode in, int targets) {
        if (log.isDebugEnabled()) {
            log.debug("confmcp: {} for userKey={} targets={} correlationId={}", what,
                    Json.text(in, "userKey"), targets, Json.text(in, "correlationId"));
        }
    }

    private static Response internalError(String what, RuntimeException e) {
        log.error("confmcp: {} failed: {}", what, e.toString());
        log.debug("confmcp: failure detail", e);
        return Json.error(500, "internal error");
    }
}
