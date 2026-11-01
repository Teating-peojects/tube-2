import type { Metadata } from "next";
import { site } from "@/config/site";
import { H, LegalPage, P, UL } from "@/features/legal/LegalPage";

export const metadata: Metadata = { title: "Copyright / DMCA", alternates: { canonical: "/dmca" } };

export default function Page() {
  return (
    <LegalPage title="Copyright / DMCA">
      <P>{site.name} respects intellectual property rights and responds to notices that comply with applicable law, including the U.S. Digital Millennium Copyright Act (17 U.S.C. § 512).</P>
      <H>Filing a notice of infringement</H>
      <P>Send a written notice to our designated agent that includes:</P>
      <UL>
        <li>Your physical or electronic signature (or that of a person authorised to act for the rights holder).</li>
        <li>Identification of the copyrighted work you claim is infringed.</li>
        <li>Identification of the material on {site.name} (the URL of each video) and enough information for us to locate it.</li>
        <li>Your name, address, telephone number and email address.</li>
        <li>A statement that you have a good-faith belief the use is not authorised by the rights holder, its agent or the law.</li>
        <li>A statement, under penalty of perjury, that the information is accurate and that you are authorised to act for the rights holder.</li>
      </UL>
      <H>Designated agent</H>
      <P>{site.legalName}, Attn: Copyright Agent, [postal address], {site.dmcaEmail}</P>
      <H>Counter-notification</H>
      <P>If you believe material was removed by mistake or misidentification, you may send a counter-notification containing your signature, identification of the removed material and its former location, a statement under penalty of perjury that you believe it was removed by mistake, and your consent to the jurisdiction of the appropriate court. We may restore the material unless the rights holder starts legal action.</P>
      <H>Repeat infringers</H>
      <P>We terminate accounts of users who are repeat infringers in appropriate circumstances.</P>
      <P>Knowingly submitting a false notice or counter-notification may have legal consequences.</P>
    </LegalPage>
  );
}
