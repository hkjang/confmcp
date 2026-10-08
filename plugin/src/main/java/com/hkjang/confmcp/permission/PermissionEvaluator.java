package com.hkjang.confmcp.permission;

import com.atlassian.confluence.core.ContentEntityObject;
import com.atlassian.confluence.core.SpaceContentEntityObject;
import com.atlassian.confluence.pages.AbstractPage;
import com.atlassian.confluence.pages.Attachment;
import com.atlassian.confluence.pages.AttachmentManager;
import com.atlassian.confluence.pages.BlogPost;
import com.atlassian.confluence.pages.Comment;
import com.atlassian.confluence.pages.CommentManager;
import com.atlassian.confluence.pages.Page;
import com.atlassian.confluence.pages.PageManager;
import com.atlassian.confluence.security.Permission;
import com.atlassian.confluence.security.PermissionManager;
import com.atlassian.confluence.security.SpacePermissionManager;
import com.atlassian.confluence.spaces.Space;
import com.atlassian.confluence.spaces.SpaceManager;
import com.atlassian.confluence.spaces.Spaced;
import com.atlassian.confluence.user.ConfluenceUser;
import com.atlassian.plugin.spring.scanner.annotation.imports.ComponentImport;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import javax.inject.Inject;
import javax.inject.Named;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;

/**
 * Answers "may this user do X to Y" using Confluence's own permission engine.
 *
 * Nothing here re-implements Confluence rules: view restrictions inherited from
 * ancestors, page-level edit restrictions (not inherited), group grants,
 * anonymous and administrator exemptions are all decided by
 * {@link PermissionManager} / {@link SpacePermissionManager}, evaluated for the
 * user named in the request, never for the caller.
 *
 * Content the user cannot view is answered exactly like content that does not
 * exist (deny / NOT_VISIBLE, no metadata), so the answer never reveals that a
 * hidden page exists.
 */
@Named("confmcpPermissionEvaluator")
public class PermissionEvaluator {

    private static final Logger log = LoggerFactory.getLogger(PermissionEvaluator.class);

    public static final String ALLOW = "allow";
    public static final String DENY = "deny";
    public static final String UNKNOWN = "unknown";

    // Confluence space permission keys (SpacePermission constants).
    static final String SPACE_EDIT = "EDITSPACE";
    static final String SPACE_COMMENT = "COMMENT";
    static final String SPACE_ATTACH = "CREATEATTACHMENT";
    static final String SPACE_REMOVE_PAGE = "REMOVEPAGE";

    private final PermissionManager permissionManager;
    private final SpacePermissionManager spacePermissionManager;
    private final SpaceManager spaceManager;
    private final PageManager pageManager;
    private final CommentManager commentManager;
    private final AttachmentManager attachmentManager;

    @Inject
    public PermissionEvaluator(@ComponentImport PermissionManager permissionManager,
                               @ComponentImport SpacePermissionManager spacePermissionManager,
                               @ComponentImport SpaceManager spaceManager,
                               @ComponentImport PageManager pageManager,
                               @ComponentImport CommentManager commentManager,
                               @ComponentImport AttachmentManager attachmentManager) {
        this.permissionManager = permissionManager;
        this.spacePermissionManager = spacePermissionManager;
        this.spaceManager = spaceManager;
        this.pageManager = pageManager;
        this.commentManager = commentManager;
        this.attachmentManager = attachmentManager;
    }

    /** A target as received; echoed back verbatim so the gateway can match answers to checks. */
    public static final class Target {
        final String kind;
        final String spaceKey;
        final String id;

        public Target(String kind, String spaceKey, String id) {
            this.kind = kind;
            this.spaceKey = spaceKey;
            this.id = id;
        }

        Map<String, Object> toJson() {
            Map<String, Object> out = new LinkedHashMap<String, Object>();
            out.put("kind", kind == null ? "" : kind);
            if (spaceKey != null && !spaceKey.isEmpty()) {
                out.put("spaceKey", spaceKey);
            }
            if (id != null && !id.isEmpty()) {
                out.put("id", id);
            }
            return out;
        }
    }

    /**
     * Evaluates one target. The result object has "target", "results",
     * "reasons" and, only when the user can view it, "content" or "space".
     */
    public Map<String, Object> evaluate(ConfluenceUser user, boolean deactivated, Target target, List<String> ops) {
        Map<String, Object> out = new LinkedHashMap<String, Object>();
        Map<String, String> results = new LinkedHashMap<String, String>();
        Map<String, String> reasons = new LinkedHashMap<String, String>();
        out.put("target", target.toJson());

        if (deactivated) {
            setAll(ops, results, reasons, DENY, "USER_DEACTIVATED");
        } else if ("content".equals(target.kind)) {
            evaluateContent(user, target, ops, out, results, reasons);
        } else if ("space".equals(target.kind)) {
            evaluateSpace(user, target, ops, out, results, reasons);
        } else {
            setAll(ops, results, reasons, UNKNOWN, "INVALID_TARGET");
        }

        out.put("results", results);
        out.put("reasons", reasons);
        return out;
    }

    // ---------------------------------------------------------------- content

    private void evaluateContent(ConfluenceUser user, Target target, List<String> ops, Map<String, Object> out,
                                 Map<String, String> results, Map<String, String> reasons) {
        ContentEntityObject ce;
        boolean visible;
        try {
            ce = load(target.id);
            visible = ce != null && permissionManager.hasPermission(user, Permission.VIEW, ce);
        } catch (RuntimeException e) {
            failAll("content", ops, results, reasons, e);
            return;
        } catch (LinkageError e) {
            failAll("content", ops, results, reasons, e);
            return;
        }
        if (!visible) {
            setAll(ops, results, reasons, DENY, "NOT_VISIBLE");
            return;
        }

        String status = status(ce);
        Space space = spaceOf(ce);
        Map<String, Object> content = new LinkedHashMap<String, Object>();
        content.put("id", ce instanceof Attachment ? "att" + ce.getId() : ce.getIdAsString());
        content.put("type", ce.getType());
        content.put("status", status);
        content.put("spaceKey", space == null ? "" : space.getKey());
        out.put("content", content);

        for (String op : ops) {
            try {
                put(results, reasons, op, decideContent(user, ce, space, "current".equals(status), op));
            } catch (RuntimeException e) {
                fail("content", op, results, reasons, e);
            } catch (LinkageError e) {
                fail("content", op, results, reasons, e);
            }
        }
    }

    private String[] decideContent(ConfluenceUser user, ContentEntityObject ce, Space space, boolean current, String op) {
        if ("read".equals(op)) {
            return allow();
        }
        if (!isKnownOp(op)) {
            return answer(UNKNOWN, "UNKNOWN_OPERATION");
        }
        if (!current) {
            // Trashed, draft and historical versions cannot be changed through the normal flow.
            return answer(DENY, "CONTENT_NOT_CURRENT");
        }
        switch (op) {
            case "edit":
                return permissionManager.hasPermission(user, Permission.EDIT, ce)
                        ? allow() : answer(DENY, editDenyReason(user, ce, space));
            case "move":
                // Moving needs edit permission on the content itself (Confluence's
                // own move check starts there); comments cannot be moved.
                if (ce instanceof Comment) {
                    return answer(DENY, "NOT_SUPPORTED");
                }
                return permissionManager.hasPermission(user, Permission.EDIT, ce)
                        ? allow() : answer(DENY, editDenyReason(user, ce, space));
            case "delete":
                return permissionManager.hasPermission(user, Permission.REMOVE, ce)
                        ? allow() : answer(DENY, "NO_DELETE_PERMISSION");
            case "create":
                if (space == null) {
                    return answer(DENY, "NOT_SUPPORTED");
                }
                Class<?> type = ce instanceof BlogPost ? BlogPost.class : Page.class;
                return permissionManager.hasCreatePermission(user, space, type)
                        ? allow() : answer(DENY, "NO_SPACE_CREATE");
            case "comment":
                return permissionManager.hasCreatePermission(user, ce, Comment.class)
                        ? allow() : answer(DENY, "NO_COMMENT_PERMISSION");
            case "attach":
                Object container = attachContainer(ce);
                if (container == null) {
                    return answer(DENY, "NOT_SUPPORTED");
                }
                return permissionManager.hasCreatePermission(user, container, Attachment.class)
                        ? allow() : answer(DENY, "NO_ATTACH");
            default:
                return answer(UNKNOWN, "UNKNOWN_OPERATION");
        }
    }

    /** Distinguishes a page restriction from a missing space permission, for the audit trail only. */
    private String editDenyReason(ConfluenceUser user, ContentEntityObject ce, Space space) {
        if (!(ce instanceof AbstractPage) || space == null) {
            return "NO_EDIT_PERMISSION";
        }
        return spacePermissionManager.hasPermission(SPACE_EDIT, space, user) ? "PAGE_EDIT_RESTRICTED" : "NO_SPACE_EDIT";
    }

    /** Attachments live on pages and blog posts; a new version of an attachment goes to its container. */
    private static Object attachContainer(ContentEntityObject ce) {
        if (ce instanceof Attachment) {
            ContentEntityObject owner = ((Attachment) ce).getContainer();
            return owner instanceof SpaceContentEntityObject && !(owner instanceof Attachment) ? owner : null;
        }
        if (ce instanceof SpaceContentEntityObject) {
            return ce;
        }
        return null;
    }

    /**
     * Loads content by REST-style id: numeric for pages, blog posts and
     * comments, optionally "att"-prefixed for attachments. Lookups here are not
     * permission checked; the caller checks VIEW before revealing anything.
     */
    ContentEntityObject load(String rawId) {
        if (rawId == null) {
            return null;
        }
        String id = rawId.trim();
        boolean attachmentOnly = false;
        if (id.length() > 3 && id.regionMatches(true, 0, "att", 0, 3)) {
            attachmentOnly = true;
            id = id.substring(3);
        }
        long n = parseId(id);
        if (n <= 0) {
            return null;
        }
        if (attachmentOnly) {
            return attachmentManager.getAttachment(n);
        }
        ContentEntityObject ce = pageManager.getAbstractPage(n);
        if (ce == null) {
            ce = commentManager.getComment(n);
        }
        if (ce == null) {
            ce = attachmentManager.getAttachment(n);
        }
        return ce;
    }

    static long parseId(String id) {
        if (id.isEmpty() || id.length() > 19) {
            return -1;
        }
        for (int i = 0; i < id.length(); i++) {
            if (id.charAt(i) < '0' || id.charAt(i) > '9') {
                return -1;
            }
        }
        try {
            return Long.parseLong(id);
        } catch (NumberFormatException e) {
            return -1;
        }
    }

    /** Status as Confluence's REST API names it. */
    private static String status(ContentEntityObject ce) {
        if (ce.isDraft()) {
            return "draft";
        }
        if (ce.isDeleted()) {
            return "trashed";
        }
        if (!ce.isLatestVersion()) {
            return "historical";
        }
        return "current";
    }

    /** Pages, blog posts, attachments (SpaceContentEntityObject) and comments are all Spaced. */
    private static Space spaceOf(ContentEntityObject ce) {
        return ce instanceof Spaced ? ((Spaced) ce).getSpace() : null;
    }

    // ------------------------------------------------------------------ space

    private void evaluateSpace(ConfluenceUser user, Target target, List<String> ops, Map<String, Object> out,
                               Map<String, String> results, Map<String, String> reasons) {
        Space space;
        boolean visible;
        try {
            String key = target.spaceKey == null ? "" : target.spaceKey.trim();
            space = key.isEmpty() ? null : spaceManager.getSpace(key);
            visible = space != null && permissionManager.hasPermission(user, Permission.VIEW, space);
        } catch (RuntimeException e) {
            failAll("space", ops, results, reasons, e);
            return;
        } catch (LinkageError e) {
            failAll("space", ops, results, reasons, e);
            return;
        }
        if (!visible) {
            setAll(ops, results, reasons, DENY, "NOT_VISIBLE");
            return;
        }

        Map<String, Object> sp = new LinkedHashMap<String, Object>();
        sp.put("key", space.getKey());
        out.put("space", sp);

        for (String op : ops) {
            try {
                put(results, reasons, op, decideSpace(user, space, op));
            } catch (RuntimeException e) {
                fail("space", op, results, reasons, e);
            } catch (LinkageError e) {
                fail("space", op, results, reasons, e);
            }
        }
    }

    private String[] decideSpace(ConfluenceUser user, Space space, String op) {
        switch (op) {
            case "read":
                return allow();
            case "create":
                return permissionManager.hasCreatePermission(user, space, Page.class)
                        ? allow() : answer(DENY, "NO_SPACE_CREATE");
            case "edit":
            case "move":
                // Space-wide: may this user add and edit pages here at all?
                // Individual pages can still carry edit restrictions.
                return permissionManager.hasCreatePermission(user, space, Page.class)
                        ? allow() : answer(DENY, "NO_SPACE_EDIT");
            case "comment":
                return permissionManager.hasCreatePermission(user, space, Comment.class)
                        ? allow() : answer(DENY, "NO_SPACE_COMMENT");
            case "attach":
                return spacePermissionManager.hasPermission(SPACE_ATTACH, space, user)
                        ? allow() : answer(DENY, "NO_ATTACH");
            case "delete":
                return spacePermissionManager.hasPermission(SPACE_REMOVE_PAGE, space, user)
                        ? allow() : answer(DENY, "NO_SPACE_DELETE");
            default:
                return answer(UNKNOWN, "UNKNOWN_OPERATION");
        }
    }

    // ---------------------------------------------------------------- helpers

    static boolean isKnownOp(String op) {
        return "read".equals(op) || "create".equals(op) || "edit".equals(op) || "comment".equals(op)
                || "attach".equals(op) || "move".equals(op) || "delete".equals(op);
    }

    private static String[] allow() {
        return new String[]{ALLOW, null};
    }

    private static String[] answer(String value, String reason) {
        return new String[]{value, reason};
    }

    private static void put(Map<String, String> results, Map<String, String> reasons, String op, String[] answer) {
        results.put(op, answer[0]);
        if (answer[1] != null) {
            reasons.put(op, answer[1]);
        } else {
            reasons.remove(op);
        }
    }

    private static void setAll(List<String> ops, Map<String, String> results, Map<String, String> reasons,
                               String value, String reason) {
        for (String op : ops) {
            results.put(op, value);
            reasons.put(op, reason);
        }
    }

    private static void failAll(String what, List<String> ops, Map<String, String> results,
                                Map<String, String> reasons, Throwable e) {
        log.warn("confmcp: {} permission lookup failed: {}", what, e.toString());
        log.debug("confmcp: lookup failure detail", e);
        setAll(ops, results, reasons, UNKNOWN, "CHECK_FAILED");
    }

    private static void fail(String what, String op, Map<String, String> results,
                             Map<String, String> reasons, Throwable e) {
        log.warn("confmcp: {} permission check for '{}' failed: {}", what, op, e.toString());
        log.debug("confmcp: check failure detail", e);
        results.put(op, UNKNOWN);
        reasons.put(op, "CHECK_FAILED");
    }
}
