# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.1-alpha.5"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.5/gestaltd-macos-arm64.tar.gz"
      sha256 "da85f964d9f5ace3ccd807dd289dc36c9a05b3f3dae0e7b93962bf27a653449b"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.5/gestaltd-macos-x86_64.tar.gz"
      sha256 "60071f8a306608968941aac153ec43187fb84b3eff1a8dec50b8c94a3f45b148"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.5/gestaltd-linux-arm64.tar.gz"
      sha256 "7d81f0ab4b008f7358581019ae30291e4ecf78313c0ab1504cad56ca01758850"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.5/gestaltd-linux-x86_64.tar.gz"
      sha256 "5b938f6995a3ef2045d17ffd1e6d731b4b929befbc4128f1d88f6f07b821a48b"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end
