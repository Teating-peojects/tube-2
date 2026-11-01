import type { Metadata } from "next";
import Link from "next/link";
import { site } from "@/config/site";
import { H, LegalPage, P, UL } from "@/features/legal/LegalPage";

export const metadata: Metadata = { title: "Community Guidelines", alternates: { canonical: "/community-guidelines" } };

export default function Page() {
  return (
    <LegalPage title="Community Guidelines">
      <P>{site.name} is a platform for adults. These rules exist to protect the people who appear in content and the people who watch it.</P>
      <H>Zero tolerance</H>
      <UL>
        <li><strong className="text-white">Anyone under 18.</strong> No content depicting, involving or sexualising minors, in any form. We remove it immediately and report it to the relevant authorities as required by law.</li>
        <li><strong className="text-white">No consent.</strong> No content filmed or shared without the clear consent of everyone shown, including hidden-camera material, “revenge” content and sexual deepfakes of real people.</li>
        <li><strong className="text-white">Coercion and exploitation.</strong> No content involving trafficking, force, threats or exploitation.</li>
      </UL>
      <H>Also not allowed</H>
      <UL>
        <li>Violence, animal harm, or content that promotes self-harm.</li>
        <li>Harassment, hate, doxxing and threats.</li>
        <li>Illegal content or activity, scams, spam and impersonation.</li>
        <li>Content you do not have the right to publish (see <Link href="/dmca" className="underline">Copyright / DMCA</Link>).</li>
      </UL>
      <H>For uploaders</H>
      <P>You must confirm that everyone in your video is an adult who consented to its creation and publication. We may ask for age and consent documentation, and where the law requires it you must keep records (for example under 18 U.S.C. § 2257 in the United States). [Describe your verification process.]</P>
      <H>Reporting</H>
      <P>Use <strong className="text-white">Report</strong> on any video, comment or channel. If someone is in immediate danger, contact your local emergency services. To request removal of content that shows you without your consent, email {site.supportEmail}.</P>
      <H>Enforcement</H>
      <P>Violations can lead to content removal, restrictions, account suspension or termination, and referral to law enforcement. If you think we made a mistake, contact {site.supportEmail} to appeal.</P>
    </LegalPage>
  );
}
