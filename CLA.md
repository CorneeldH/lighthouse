# Contributor License Agreement

**Revision: 2026-08-24**

Thank you for your interest in contributing to this project (the "Project"), maintained by
Corneel den Hartogh (the "Project Owner").

This Contributor License Agreement ("Agreement") documents the rights granted by contributors to
the Project Owner. It is derived from the Apache Software Foundation Individual Contributor
License Agreement v2.0, with the deviations listed in Section 12. **You keep the copyright in
your Contributions.** This Agreement grants a licence; it is not an assignment.

Please read this document carefully. By signing (see Section 11), you accept and agree to the
following terms and conditions for Your present and future Contributions submitted to the
Project. Except for the licence granted herein to the Project Owner and recipients of software
distributed by the Project Owner, You reserve all right, title, and interest in and to Your
Contributions.

## 1. Definitions

"You" (or "Your") means the copyright owner or legal entity authorized by the copyright owner
that is making this Agreement with the Project Owner. For legal entities, the entity making a
Contribution and all other entities that control, are controlled by, or are under common control
with that entity are considered to be a single Contributor. For the purposes of this definition,
"control" means (i) the power, direct or indirect, to cause the direction or management of such
entity, whether by contract or otherwise, or (ii) ownership of fifty percent (50%) or more of the
outstanding shares, or (iii) beneficial ownership of such entity.

"Project Owner" means Corneel den Hartogh, and his successors and assigns.

"Contribution" means any original work of authorship, including any modifications or additions to
an existing work, that is intentionally submitted by You to the Project Owner for inclusion in,
or documentation of, any of the products owned or managed by the Project Owner (the "Work"). For
the purposes of this definition, "submitted" means any form of electronic, verbal, or written
communication sent to the Project Owner or its representatives, including but not limited to
communication on electronic mailing lists, source code control systems, and issue tracking
systems that are managed by, or on behalf of, the Project Owner for the purpose of discussing and
improving the Work, but excluding communication that is conspicuously marked or otherwise
designated in writing by You as "Not a Contribution."

## 2. Grant of Copyright License

Subject to the terms and conditions of this Agreement, You hereby grant to the Project Owner and
to recipients of software distributed by the Project Owner a perpetual, worldwide,
non-exclusive, no-charge, royalty-free, irrevocable copyright license to reproduce, prepare
derivative works of, publicly display, publicly perform, sublicense, and distribute Your
Contributions and such derivative works.

## 3. Grant of Patent License

Subject to the terms and conditions of this Agreement, You hereby grant to the Project Owner and
to recipients of software distributed by the Project Owner a perpetual, worldwide,
non-exclusive, no-charge, royalty-free, irrevocable (except as stated in this section) patent
license to make, have made, use, offer to sell, sell, import, and otherwise transfer the Work,
where such license applies only to those patent claims licensable by You that are necessarily
infringed by Your Contribution(s) alone or by combination of Your Contribution(s) with the Work
to which such Contribution(s) was submitted. If any entity institutes patent litigation against
You or any other entity (including a cross-claim or counterclaim in a lawsuit) alleging that
your Contribution, or the Work to which you have contributed, constitutes direct or contributory
patent infringement, then any patent licenses granted to that entity under this Agreement for
that Contribution or Work shall terminate as of the date such litigation is filed.

## 4. Entitlement and Employer

You represent that you are legally entitled to grant the above license. If your employer(s) has
rights to intellectual property that you create that includes your Contributions, you represent
that you have received permission to make Contributions on behalf of that employer, that your
employer has waived such rights for your Contributions to the Project Owner, or that your
employer has executed a separate corporate contributor licence agreement with the Project Owner.

## 5. Original Creation

You represent that each of Your Contributions is Your original creation (see Section 6 for
submissions on behalf of others). You represent that Your Contribution submissions include
complete details of any third-party license or other restriction (including, but not limited to,
related patents and trademarks) of which you are personally aware and which are associated with
any part of Your Contributions.

Where a Contribution was produced with the assistance of an AI tool, You represent that You are
nevertheless entitled to submit it under the terms of this Agreement, and that doing so does not
knowingly infringe the rights of any third party.

## 6. Third-Party Submissions

Should You wish to submit work that is not Your original creation, You may submit it to the
Project Owner separately from any Contribution, identifying the complete details of its source
and of any license or other restriction (including, but not limited to, related patents,
trademarks, and license agreements) of which you are personally aware, and conspicuously marking
the work as "Submitted on behalf of a third-party: [named here]".

## 7. Support and Disclaimer

You are not expected to provide support for Your Contributions, except to the extent You desire
to provide support. You may provide support for free, for a fee, or not at all. Unless required
by applicable law or agreed to in writing, You provide Your Contributions on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied, including, without
limitation, any warranties or conditions of TITLE, NON-INFRINGEMENT, MERCHANTABILITY, or FITNESS
FOR A PARTICULAR PURPOSE.

## 8. Notification

You agree to notify the Project Owner of any facts or circumstances of which you become aware
that would make these representations inaccurate in any respect.

## 9. Outbound Licensing

You acknowledge and agree that the Project Owner may license, distribute and otherwise exploit
Your Contributions, and any derivative works thereof, under any terms, including terms differing
from the Project's then-current licence, and including proprietary or commercial terms, and may
do so on an exclusive or non-exclusive basis, with or without a fee.

This Section is the operative reason this Agreement exists: it allows the Project to change its
outbound licence, and to offer commercial licences, without collecting a fresh permission from
every contributor. It does not take your copyright, and it does not restrict what You may do with
Your own Contributions.

## 10. Governing Law

This Agreement is governed by the laws of the Netherlands, without regard to its conflict of law
provisions. Any dispute arising out of or in connection with this Agreement shall be submitted to
the exclusive jurisdiction of the competent court in Utrecht, the Netherlands, insofar as
mandatory law permits.

## 11. How to Sign

**A signature is a commit that adds your own entry to `.github/cla-signatures.json`.** There is
no external service and no separate database: the record lives in this repository, in git,
alongside the contribution it covers.

Add one object to the `signatures` array:

```json
{
  "name": "Your Full Name",
  "github_login": "your-github-login",
  "emails": ["the-email-you-commit-with@example.com"],
  "date": "2026-09-01",
  "cla_revision": "2026-08-24",
  "agreement": "I have read the CLA document and I hereby sign the CLA."
}
```

- `emails` must list **every** address you author or commit with, exactly as it appears in
  `git log`. If you use GitHub's private commit address, that is the one to list. Listing more
  than one is fine.
- `agreement` must be the sentence above, byte for byte. Anything else fails the check.
- `cla_revision` must be a revision listed in `cla_revisions` in the register.
- Signatures may only be **added**. Removing or editing an existing entry silently withdraws a
  grant and is rejected by the check.

Two routes, both valid:

1. **In the same pull request as your contribution** — the check reads the register from your
   working tree, so one PR can carry both.
2. **Up front, in a separate one-file pull request** — useful if you want the paperwork settled
   before you write code.

The register is public and contains your name and e-mail address. That is the same information a
git contribution publishes anyway.

## 12. Deviations from the Apache ICLA v2

For readers who know the original, this Agreement differs in five places:

1. **Party** — the Project Owner ("Corneel den Hartogh, and his successors and assigns") replaces
   the Apache Software Foundation. The successors clause lets the grant move to a later legal
   entity without anyone re-signing.
2. **Section 9, Outbound Licensing** — new. It makes relicensing and dual licensing explicit
   rather than inferred from the word "sublicense" in Section 2.
3. **Section 10, Governing Law** — new. The Apache ICLA has no choice of law.
4. **Section 5** — one sentence added on AI-assisted contributions.
5. **Section 11, How to Sign** — new, and specific to this repository.

Sections 3 (patent grant, including defensive termination), 6 (third-party submissions) and 7
("AS IS") are unchanged in substance.

## Not legal advice

This document was written by the project author for the project author. It is offered as a
practical instrument, not as legal advice, and it has not been reviewed by counsel.
