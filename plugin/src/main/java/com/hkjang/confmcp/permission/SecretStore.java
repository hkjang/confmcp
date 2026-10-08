package com.hkjang.confmcp.permission;

import com.atlassian.plugin.spring.scanner.annotation.imports.ComponentImport;
import com.atlassian.sal.api.pluginsettings.PluginSettings;
import com.atlassian.sal.api.pluginsettings.PluginSettingsFactory;
import com.atlassian.sal.api.transaction.TransactionCallback;
import com.atlassian.sal.api.transaction.TransactionTemplate;

import javax.inject.Inject;
import javax.inject.Named;

/**
 * Holds the secret shared with the confmcp gateway.
 *
 * Primary storage is SAL global plugin settings under {@link #SETTINGS_KEY},
 * written by a system administrator through {@code PUT /rest/confmcp/1.0/config}.
 * The JVM system property {@code -Dconfmcp.secret} is honoured as a fallback.
 * Without either, every signed endpoint answers 503 so a half-finished
 * install never serves permission data.
 */
@Named("confmcpSecretStore")
public class SecretStore {

    public static final String SETTINGS_KEY = "com.hkjang.confmcp.secret";
    public static final String SYSTEM_PROPERTY = "confmcp.secret";
    public static final int MIN_LENGTH = 16;
    public static final int MAX_LENGTH = 512;

    public enum Source { PLUGIN_SETTINGS, SYSTEM_PROPERTY, NONE }

    private final PluginSettingsFactory pluginSettingsFactory;
    private final TransactionTemplate transactionTemplate;

    @Inject
    public SecretStore(@ComponentImport PluginSettingsFactory pluginSettingsFactory,
                       @ComponentImport TransactionTemplate transactionTemplate) {
        this.pluginSettingsFactory = pluginSettingsFactory;
        this.transactionTemplate = transactionTemplate;
    }

    /** The active secret, or null when none is configured. */
    public String secret() {
        String stored = stored();
        if (stored != null) {
            return stored;
        }
        String prop = System.getProperty(SYSTEM_PROPERTY);
        return prop == null || prop.trim().isEmpty() ? null : prop.trim();
    }

    public Source source() {
        if (stored() != null) {
            return Source.PLUGIN_SETTINGS;
        }
        String prop = System.getProperty(SYSTEM_PROPERTY);
        return prop == null || prop.trim().isEmpty() ? Source.NONE : Source.SYSTEM_PROPERTY;
    }

    /** Stores a new secret; null or empty removes the stored value. */
    public void store(final String secret) {
        transactionTemplate.execute(new TransactionCallback<Void>() {
            @Override
            public Void doInTransaction() {
                PluginSettings settings = pluginSettingsFactory.createGlobalSettings();
                if (secret == null || secret.isEmpty()) {
                    settings.remove(SETTINGS_KEY);
                } else {
                    settings.put(SETTINGS_KEY, secret);
                }
                return null;
            }
        });
    }

    private String stored() {
        Object value = pluginSettingsFactory.createGlobalSettings().get(SETTINGS_KEY);
        if (value instanceof String && !((String) value).trim().isEmpty()) {
            return ((String) value).trim();
        }
        return null;
    }
}
