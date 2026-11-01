import type { Metadata } from "next";
import { site } from "@/config/site";
import { H, LegalPage, P, UL } from "@/features/legal/LegalPage";

export const metadata: Metadata = { title: "Privacy Policy", alternates: { canonical: "/privacy" } };

export default function Page() {
  return (
    <LegalPage title="Privacy Policy">
      <P>This policy explains what {site.legalName} collects when you use {site.name}, why, and the choices you have.</P>
      <H>Information we collect</H>
      <UL>
        <li><strong className="text-white">Account data:</strong> email, username, display name, password (stored hashed), profile details you add.</li>
        <li><strong className="text-white">Content and activity:</strong> videos, comments, playlists, likes, subscriptions, watch history (if enabled), reports you submit.</li>
        <li><strong className="text-white">Technical data:</strong> IP address, device and browser information, and logs needed for security and abuse prevention.</li>
      </UL>
      <H>Cookies and local storage</H>
      <UL>
        <li><code>xh_age_ok</code>: remembers that you confirmed you are 18+ (about 30 days).</li>
        <li>Session cookie (httpOnly): keeps you logged in.</li>
        <li>Local storage (<code>xh-ui</code>, <code>xh-recent</code>): your interface preferences and recent searches, kept on your device only.</li>
      </UL>
      <H>How we use information</H>
      <P>To provide and secure the Service, personalise recommendations, process uploads, moderate content, comply with legal obligations and respond to requests.</P>
      <H>Sharing</H>
      <P>We share data with service providers that help us operate the Service (hosting, storage, video delivery, analytics), when required by law, and to protect people and the Service. We do not sell your personal information. [Confirm against your actual practices.]</P>
      <H>Retention and security</H>
      <P>We keep data only as long as needed for the purposes above or as the law requires, and protect it with technical and organisational safeguards.</P>
      <H>Your choices and rights</H>
      <UL>
        <li>Manage privacy, notifications and history in Settings, and delete your account at any time.</li>
        <li>Depending on where you live you may have rights to access, correct, export or delete your data. Contact us to exercise them.</li>
      </UL>
      <H>Age</H>
      <P>The Service is for adults only. We do not knowingly collect data from anyone under 18 and will delete it if we learn we have.</P>
      <H>Contact</H>
      <P>{site.supportEmail}</P>
    </LegalPage>
  );
}
