package com.hkjang.confmcp.permission;

import org.codehaus.jackson.JsonNode;
import org.codehaus.jackson.map.ObjectMapper;

import javax.ws.rs.core.Response;
import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.io.InputStream;
import java.util.ArrayList;
import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/** JSON in and out, done here so responses look the same on every Confluence build. */
final class Json {

    static final String CONTENT_TYPE = "application/json;charset=UTF-8";
    static final int MAX_BODY_BYTES = 1 << 20;

    private static final ObjectMapper MAPPER = new ObjectMapper();

    private Json() {
    }

    /** Thrown for bodies that are too large or not JSON; carries the HTTP status to answer with. */
    static final class BadRequest extends Exception {
        private static final long serialVersionUID = 1L;
        final int status;

        BadRequest(int status, String message) {
            super(message);
            this.status = status;
        }
    }

    static byte[] readBody(InputStream in) throws BadRequest {
        if (in == null) {
            return new byte[0];
        }
        ByteArrayOutputStream out = new ByteArrayOutputStream();
        byte[] buf = new byte[8192];
        try {
            int n;
            while ((n = in.read(buf)) != -1) {
                out.write(buf, 0, n);
                if (out.size() > MAX_BODY_BYTES) {
                    throw new BadRequest(413, "request body too large");
                }
            }
        } catch (IOException e) {
            throw new BadRequest(400, "unreadable request body");
        }
        return out.toByteArray();
    }

    static JsonNode parse(byte[] body) throws BadRequest {
        try {
            JsonNode node = body.length == 0 ? null : MAPPER.readTree(body);
            if (node == null || !node.isObject()) {
                throw new BadRequest(400, "request body must be a JSON object");
            }
            return node;
        } catch (IOException e) {
            throw new BadRequest(400, "invalid JSON body");
        }
    }

    /** Text of a field, or null when absent or not a string. */
    static String text(JsonNode node, String field) {
        if (node == null) {
            return null;
        }
        JsonNode v = node.get(field);
        return v != null && v.isTextual() ? v.getTextValue() : null;
    }

    static List<String> strings(JsonNode node, String field) {
        JsonNode v = node == null ? null : node.get(field);
        if (v == null || !v.isArray()) {
            return Collections.emptyList();
        }
        List<String> out = new ArrayList<String>(v.size());
        for (JsonNode item : v) {
            if (item.isTextual()) {
                out.add(item.getTextValue());
            }
        }
        return out;
    }

    static Response ok(Object entity) {
        return respond(200, entity);
    }

    static Response error(int status, String message) {
        Map<String, Object> out = new LinkedHashMap<String, Object>();
        out.put("message", message);
        return respond(status, out);
    }

    static Response error(BadRequest e) {
        return error(e.status, e.getMessage());
    }

    static Response respond(int status, Object entity) {
        String json;
        try {
            json = MAPPER.writeValueAsString(entity);
        } catch (IOException e) {
            status = 500;
            json = "{\"message\":\"internal error\"}";
        }
        return Response.status(status)
                .type(CONTENT_TYPE)
                .header("Cache-Control", "no-store")
                .entity(json)
                .build();
    }
}
