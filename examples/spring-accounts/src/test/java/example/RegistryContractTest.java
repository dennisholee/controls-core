package example;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import org.junit.jupiter.api.Test;
import org.yaml.snakeyaml.Yaml;

import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;

import static org.junit.jupiter.api.Assertions.assertTrue;

/**
 * Standard-build drift check: the committed registry must match the
 * x-resource-* annotations in openapi.yaml. Generation itself is a pipeline
 * step (openapi-registry); this test keeps mvn verify honest without Go.
 */
class RegistryContractTest {

    @Test
    void annotationsMatchRegistry() throws Exception {
        Map<String, Object> spec = new Yaml().load(Files.newInputStream(Path.of("openapi.yaml")));
        JsonNode registry = new ObjectMapper().readTree(Path.of("controls/registry.json").toFile());
        List<String> problems = new ArrayList<>();

        Map<?, ?> paths = (Map<?, ?>) spec.get("paths");
        for (Map.Entry<?, ?> pathEntry : paths.entrySet()) {
            String path = (String) pathEntry.getKey();
            Map<?, ?> item = (Map<?, ?>) pathEntry.getValue();
            for (Map.Entry<?, ?> opEntry : item.entrySet()) {
                String method = ((String) opEntry.getKey()).toUpperCase();
                if (!(opEntry.getValue() instanceof Map<?, ?> op)) continue;
                if (Boolean.TRUE.equals(op.get("x-controls-ignore"))) continue;
                if (!op.containsKey("x-resource-type")) continue;

                JsonNode ep = endpoint(registry, method, path);
                if (ep == null) {
                    problems.add(method + " " + path + ": no registry entry");
                    continue;
                }
                compare(problems, method + " " + path, op, ep);
            }
        }
        assertTrue(problems.isEmpty(), String.join("\n", problems));
    }

    private static JsonNode endpoint(JsonNode registry, String method, String path) {
        for (JsonNode ep : registry.get("endpoints")) {
            if (method.equalsIgnoreCase(ep.get("method").asText()) && path.equals(ep.get("path").asText())) {
                return ep;
            }
        }
        return null;
    }

    private static void compare(List<String> problems, String where, Map<?, ?> op, JsonNode ep) {
        check(problems, where, "x-resource-type", op.get("x-resource-type"), ep.path("resourceType").asText());
        check(problems, where, "x-mask-policy", op.get("x-mask-policy"), ep.path("maskPolicy").asText());
        check(problems, where, "x-resource-source", op.get("x-resource-source"), ep.path("resourceSource").asText());

        Object id = op.get("x-resource-id");
        if (id instanceof String s && !s.isEmpty()) {
            JsonNode ref = ep.path("resourceId");
            if (s.startsWith("$.")) {
                if (!"body".equals(ref.path("from").asText()) || !s.equals(ref.path("path").asText())) {
                    problems.add(where + ": x-resource-id " + s + " != " + ref);
                }
            } else if (!"path".equals(ref.path("from").asText()) || !s.equals(ref.path("name").asText())) {
                problems.add(where + ": x-resource-id " + s + " != " + ref);
            }
        }

        Object attrs = op.get("x-attributes");
        if (attrs instanceof List<?> list) {
            JsonNode regAttrs = ep.path("attributes");
            if (regAttrs.size() != list.size()) {
                problems.add(where + ": x-attributes size mismatch");
            } else {
                for (int i = 0; i < list.size(); i++) {
                    if (!list.get(i).toString().equals(regAttrs.get(i).asText())) {
                        problems.add(where + ": x-attributes mismatch at " + i);
                    }
                }
            }
        }
    }

    private static void check(List<String> problems, String where, String key, Object want, String got) {
        if (want instanceof String s && !s.isEmpty() && !s.equals(got)) {
            problems.add(where + ": " + key + " " + s + " != " + got);
        }
    }
}
