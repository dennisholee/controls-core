package example;

import org.springframework.boot.SpringApplication;
import org.springframework.boot.autoconfigure.SpringBootApplication;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.RestController;

import java.util.Map;

@SpringBootApplication
@RestController
public class AccountsApplication {

    @GetMapping("/accounts/{id}")
    public Map<String, Object> account() {
        return Map.of(
            "ownerId", "u-1",
            "accountNumber", "1234567890",
            "holder", Map.of("email", "jane.doe@example.com"),
            "ssn", "111-22-3333",
            "region", "US",
            "status", "active"
        );
    }

    public static void main(String[] args) {
        SpringApplication.run(AccountsApplication.class, args);
    }
}
