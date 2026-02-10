# Shared Spaces (Design Spec)

## Overview
Shared spaces allow teams to collaborate on notebooks with clear ownership, controlled access, and traceable activity. A space is a container that can hold notebooks, pages, assets, and related metadata for a team or project.

## Goals
- Enable multiple people to work in a shared space with defined roles.
- Provide flexible sharing (direct invites and link sharing).
- Maintain an auditable record of access and actions.

## Non-Goals
- Real-time co-editing conflict resolution (handled elsewhere).
- External guest billing or monetization logic.

## Core Concepts
- **Space**: A shared container for notebooks.
- **Member**: A user with a role in a space.
- **Role**: Determines permissions (owner/editor/viewer).
- **Share Link**: A URL granting access based on link policy.
- **Audit Log**: Immutable record of membership and key actions.

## Roles & Permissions
| Capability | Owner | Editor | Viewer |
| --- | --- | --- | --- |
| View space contents | ✅ | ✅ | ✅ |
| Create notebooks | ✅ | ✅ | ❌ |
| Edit notebooks | ✅ | ✅ | ❌ |
| Delete notebooks | ✅ | ✅ (own content only) | ❌ |
| Manage members/roles | ✅ | ❌ | ❌ |
| Configure sharing settings | ✅ | ❌ | ❌ |
| View audit log | ✅ | ✅ (read-only) | ❌ |
| Transfer ownership | ✅ | ❌ | ❌ |

### Role Notes
- **Owner**: Ultimate authority for the space. Can change settings, manage members, and revoke access.
- **Editor**: Can create and edit content. Cannot change membership or sharing settings.
- **Viewer**: Read-only access to content.

## Sharing Models
### Direct Invitations
- Owners can invite users by email or username.
- Invitees are assigned a default role (configurable per space; default: viewer).
- Invitations can be accepted or declined; pending invitations appear in the membership list.

### Link Sharing
- Owners can enable a share link for the space.
- Link options:
  - **Off**: No link access.
  - **Anyone with the link (viewer)**: Unauthenticated or authenticated users can view.
  - **Anyone with the link (editor)**: Users can edit (requires sign-in).
  - **Organization only**: Link works only for users in the same org.
- Link options must indicate whether sign-in is required and what role the link grants.
- Link access can be revoked at any time; revocation invalidates prior link tokens.

## Membership & Access Rules
- A user can hold only one role per space.
- Owners can promote/demote editors and viewers.
- Space access is determined by the highest privilege from:
  1. Explicit membership role.
  2. Valid share link role (if enabled).
- Removing a member revokes access immediately.

## Sharing Flows
### Invite Flow
1. Owner selects “Share” → “Invite members.”
2. Enter email/username and select role.
3. System sends invitation and records an audit entry.
4. Invitee accepts; membership becomes active.

### Link Sharing Flow
1. Owner opens “Share” settings.
2. Toggle link sharing on/off and set link role.
3. System generates a link token and displays it.
4. Recipients access the link; system checks policy and applies role.

## Auditing
### Audit Log Events
- Space created.
- Member invited/added/removed.
- Role changed.
- Share link enabled/disabled/role changed.
- Ownership transferred.
- Notebooks created/deleted.

### Audit Log Requirements
- Immutable, append-only log.
- Each event includes timestamp, actor, target, and metadata (role changes, link settings).
- Owners and editors can view the audit log.

## Security & Compliance
- Link tokens are random, non-guessable, and revocable.
- Links expire optionally (configurable by owner, default: no expiration).
- All access checks enforce least privilege.
- Audit logs retained for at least 1 year.

## Open Questions
- Should editors be allowed to view audit logs or only owners?
- Should link sharing be disabled by default for new spaces?
