# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.2-alpha.43"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.43/gestaltd-macos-arm64.tar.gz"
      sha256 "c8a29bf289cdd2554391537c1f22cf9a7ba7313f9b3f23395877169450b934bb"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.43/gestaltd-macos-x86_64.tar.gz"
      sha256 "0be9523033da6f4522afea4ee284eefff4b5bd4cd77abf6dd8645a4f74a1c939"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.43/gestaltd-linux-arm64.tar.gz"
      sha256 "ed01f4f4b35c69cc4699bc583e3b6ea133ba2ac9066f02c563341b391c870522"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.2-alpha.43/gestaltd-linux-x86_64.tar.gz"
      sha256 "77df74a4d1cc5ca90e08489a45545843119533bc743d8270abb4542b30a65528"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end
