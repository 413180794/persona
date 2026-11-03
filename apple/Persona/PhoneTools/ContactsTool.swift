import Contacts
import Foundation

/// `phone_contacts_search`: find people in the address book by name, company
/// or phone number.
enum ContactsTool {
    static var tools: [PhoneTool] {
        [
            PhoneTool(
                spec: ToolSpec(
                    name: "phone_contacts_search",
                    description: """
                    Search the phone's contacts by name, company, job title, email or phone number. \
                    Returns up to `limit` people with their name, company, job title, phone numbers \
                    (with labels such as mobile/work), email addresses and birthday (yyyy-MM-dd). \
                    Use this to look up a colleague's number before calling or drafting a message.
                    """,
                    inputSchema: Schema.object([
                        "query": Schema.string("Text to match against name, company, job title, email or phone number. Matching is case-insensitive and looks for the text anywhere in the field; a phone number matches with or without spaces and dashes. Pass an empty string to get the first contacts in the address book's own order."),
                        "limit": Schema.integer("Maximum number of contacts to return, default 10, max 50.", min: 1, max: 50),
                    ], required: ["query"])
                ),
                source: .contacts,
                run: { args in try await search(args) }
            )
        ]
    }

    static func search(_ args: JSONValue) async throws -> [ToolContent] {
        try ContactsAccess.requireAccess()
        let query = ToolArgs.string(args, "query") ?? ""
        let limit = ToolArgs.int(args, "limit", fallback: 10, min: 1, max: 50)

        // CNContactStore is synchronous and can take a while on large address
        // books, so keep it off the main actor.
        let people: [JSONValue] = try await withCheckedThrowingContinuation { continuation in
            DispatchQueue.global(qos: .userInitiated).async {
                do {
                    continuation.resume(returning: try fetch(query: query, limit: limit))
                } catch {
                    continuation.resume(throwing: error)
                }
            }
        }
        if people.isEmpty {
            return Output.json(.object([
                "query": .string(query),
                "count": .number(0),
                "results": .array([]),
                "note": .string("No contacts matched. Try a shorter query, or the company or phone number instead."),
            ]))
        }
        return Output.json(.object([
            "query": .string(query),
            "count": .number(Double(people.count)),
            "results": .array(people),
        ]))
    }

    // Runs off the main actor (see search), so it must not be actor isolated.
    nonisolated private static func fetch(query: String, limit: Int) throws -> [JSONValue] {
        let store = CNContactStore()
        let keys: [any CNKeyDescriptor] = [
            CNContactIdentifierKey as CNKeyDescriptor,
            CNContactGivenNameKey as CNKeyDescriptor,
            CNContactMiddleNameKey as CNKeyDescriptor,
            CNContactFamilyNameKey as CNKeyDescriptor,
            CNContactNicknameKey as CNKeyDescriptor,
            CNContactOrganizationNameKey as CNKeyDescriptor,
            CNContactJobTitleKey as CNKeyDescriptor,
            CNContactPhoneNumbersKey as CNKeyDescriptor,
            CNContactEmailAddressesKey as CNKeyDescriptor,
            CNContactBirthdayKey as CNKeyDescriptor,
            CNContactFormatter.descriptorForRequiredKeys(for: .fullName),
        ]
        let request = CNContactFetchRequest(keysToFetch: keys)
        request.sortOrder = .userDefault
        request.mutableObjects = false

        var out: [JSONValue] = []
        var seen = Set<String>()
        try store.enumerateContacts(with: request) { contact, stop in
            guard out.count < limit else {
                stop.pointee = true
                return
            }
            guard seen.insert(contact.identifier).inserted else { return }
            guard query.isEmpty || Self.matches(contact, query: query) else { return }
            out.append(Self.encode(contact))
        }
        return out
    }

    nonisolated private static func matches(_ contact: CNContact, query: String) -> Bool {
        let needle = query.lowercased()
        func hit(_ value: String) -> Bool {
            !value.isEmpty && value.lowercased().contains(needle)
        }
        if hit(CNContactFormatter.string(from: contact, style: .fullName) ?? "") { return true }
        if hit(contact.nickname) || hit(contact.organizationName) || hit(contact.jobTitle) { return true }
        if contact.phoneNumbers.contains(where: { hit($0.value.stringValue) }) { return true }
        if contact.emailAddresses.contains(where: { hit($0.value as String) }) { return true }
        // A phone query often arrives with spaces or dashes the .stringValue keeps.
        let digits = query.filter(\.isNumber)
        if digits.count >= 3, contact.phoneNumbers.contains(where: { $0.value.stringValue.filter(\.isNumber).contains(digits) }) {
            return true
        }
        return false
    }

    nonisolated private static func encode(_ contact: CNContact) -> JSONValue {
        let name = CNContactFormatter.string(from: contact, style: .fullName) ?? ""
        var birthday: String?
        if let date = contact.birthday, let month = date.month, let day = date.day {
            // Contacts can store a birthday without a year; don't invent one.
            if let year = date.year {
                birthday = String(format: "%04d-%02d-%02d", year, month, day)
            } else {
                birthday = String(format: "--%02d-%02d", month, day)
            }
        }
        let phones: [JSONValue] = contact.phoneNumbers.map { item in
            .object([
                "label": .string(CNLabeledValue<NSString>.localizedString(forLabel: item.label ?? "")),
                "number": .string(item.value.stringValue),
            ])
        }
        let emails: [JSONValue] = contact.emailAddresses.map { item in
            .object([
                "label": .string(CNLabeledValue<NSString>.localizedString(forLabel: item.label ?? "")),
                "email": .string(item.value as String),
            ])
        }
        return Output.object([
            ("id", .string(contact.identifier)),
            ("name", Output.opt(name)),
            ("nickname", Output.opt(contact.nickname)),
            ("company", Output.opt(contact.organizationName)),
            ("job_title", Output.opt(contact.jobTitle)),
            ("phones", phones.isEmpty ? nil : .array(phones)),
            ("emails", emails.isEmpty ? nil : .array(emails)),
            ("birthday", Output.opt(birthday)),
        ])
    }
}
